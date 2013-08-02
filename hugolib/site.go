// Copyright © 2013 Steve Francia <spf@spf13.com>.
//
// Licensed under the Simple Public License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://opensource.org/licenses/Simple-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hugolib

import (
	"bitbucket.org/pkg/inflect"
	"bytes"
	"errors"
	"fmt"
	"github.com/spf13/nitro"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
	//"sync"
)

const slash = string(os.PathSeparator)

type Site struct {
	c           Config
	Pages       Pages
	Tmpl        *template.Template
	Indexes     IndexList
	Files       []string
	Directories []string
	Sections    Index
	Info        SiteInfo
	Shortcodes  map[string]ShortcodeFunc
	timer       *nitro.B
}

type SiteInfo struct {
	BaseUrl    template.URL
	Indexes    OrderedIndexList
	Recent     *Pages
	LastChange time.Time
	Title      string
	Config     *Config
}

func (s *Site) getFromIndex(kind string, name string) Pages {
	return s.Indexes[kind][name]
}

func NewSite(config *Config) *Site {
	return &Site{c: *config, timer: nitro.Initalize()}
}

func (site *Site) Build() (err error) {
	if err = site.Process(); err != nil {
		return
	}
	if err = site.Render(); err != nil {
		return
	}
	site.Write()
	return nil
}

func (site *Site) Analyze() {
	site.Process()
	site.checkDescriptions()
}

func (site *Site) Process() (err error) {
	site.initialize()
	site.prepTemplates()
	site.timer.Step("initialize & template prep")
	site.CreatePages()
	site.setupPrevNext()
	site.timer.Step("import pages")
	if err = site.BuildSiteMeta(); err != nil {
		return
	}
	site.timer.Step("build indexes")
	return
}

func (site *Site) Render() (err error) {
	site.ProcessShortcodes()
	site.timer.Step("render shortcodes")
	site.AbsUrlify()
	site.timer.Step("absolute URLify")
	site.RenderIndexes()
	site.RenderIndexesIndexes()
	site.timer.Step("render and write indexes")
	site.RenderLists()
	site.timer.Step("render and write lists")
	if err = site.RenderPages(); err != nil {
		return
	}
	site.timer.Step("render pages")
	site.RenderHomePage()
	site.timer.Step("render and write homepage")
	return
}

func (site *Site) Write() {
	site.WritePages()
	site.timer.Step("write pages")
}

func (site *Site) checkDescriptions() {
	for _, p := range site.Pages {
		if len(p.Description) < 60 {
			fmt.Print(p.FileName + " ")
		}
	}
}

func (s *Site) prepTemplates() {
	var templates = template.New("")

	funcMap := template.FuncMap{
		"urlize":    Urlize,
		"gt":        Gt,
		"isset":     IsSet,
		"echoParam": ReturnWhenSet,
	}

	templates.Funcs(funcMap)

	walker := func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			PrintErr("Walker: ", err)
			return nil
		}

		if !fi.IsDir() {
			filetext, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(filetext)
			name := path[len(s.c.GetAbsPath(s.c.LayoutDir))+1:]
			t := templates.New(name)
			template.Must(t.Parse(text))
		}
		return nil
	}

	filepath.Walk(s.c.GetAbsPath(s.c.LayoutDir), walker)

	s.Tmpl = templates
}

func (s *Site) initialize() {
	site := s

	s.checkDirectories()

	walker := func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			PrintErr("Walker: ", err)
			return nil
		}

		if fi.IsDir() {
			site.Directories = append(site.Directories, path)
			return nil
		} else {
			site.Files = append(site.Files, path)
			return nil
		}
		return nil
	}

	filepath.Walk(s.c.GetAbsPath(s.c.ContentDir), walker)

	s.Info = SiteInfo{BaseUrl: template.URL(s.c.BaseUrl), Title: s.c.Title, Config: &s.c}

	s.Shortcodes = make(map[string]ShortcodeFunc)
}

func (s *Site) checkDirectories() {
	if b, _ := dirExists(s.c.GetAbsPath(s.c.LayoutDir)); !b {
		FatalErr("No layout directory found, expecting to find it at " + s.c.GetAbsPath(s.c.LayoutDir))
	}
	if b, _ := dirExists(s.c.GetAbsPath(s.c.ContentDir)); !b {
		FatalErr("No source directory found, expecting to find it at " + s.c.GetAbsPath(s.c.ContentDir))
	}
	mkdirIf(s.c.GetAbsPath(s.c.PublishDir))
}

func (s *Site) ProcessShortcodes() {
	for i, _ := range s.Pages {
		s.Pages[i].Content = template.HTML(ShortcodesHandle(string(s.Pages[i].Content), s.Pages[i], s.Tmpl))
	}
}

func (s *Site) AbsUrlify() {
	for i, _ := range s.Pages {
		content := string(s.Pages[i].Content)
		content = strings.Replace(content, " src=\"/", " src=\""+s.c.BaseUrl, -1)
		content = strings.Replace(content, " src='/", " src='"+s.c.BaseUrl, -1)
		content = strings.Replace(content, " href='/", " href='"+s.c.BaseUrl, -1)
		content = strings.Replace(content, " href=\"/", " href=\""+s.c.BaseUrl, -1)
		baseWithoutTrailingSlash := strings.TrimRight(s.c.BaseUrl, "/")
		content = strings.Replace(content, baseWithoutTrailingSlash+"//", baseWithoutTrailingSlash+"/", -1)
		s.Pages[i].Content = template.HTML(content)
	}
}

func (s *Site) CreatePages() {
	for _, fileName := range s.Files {
		page := NewPage(fileName)
		page.Site = s.Info
		page.Tmpl = s.Tmpl
		s.setOutFile(page)
		if s.c.BuildDrafts || !page.Draft {
			s.Pages = append(s.Pages, page)
		}
	}

	s.Pages.Sort()
}

func (s *Site) setupPrevNext() {
	for i, _ := range s.Pages {
		if i < len(s.Pages)-1 {
			s.Pages[i].Next = s.Pages[i+1]
		}

		if i > 0 {
			s.Pages[i].Prev = s.Pages[i-1]
		}
	}
}

func (s *Site) BuildSiteMeta() (err error) {
	s.Indexes = make(IndexList)
	s.Sections = make(Index)

	for _, plural := range s.c.Indexes {
		s.Indexes[plural] = make(Index)
		for i, p := range s.Pages {
			vals := p.GetParam(plural)

			if vals != nil {
				for _, idx := range vals.([]string) {
					s.Indexes[plural].Add(idx, s.Pages[i])
				}
			}
		}
		for k, _ := range s.Indexes[plural] {
			s.Indexes[plural][k].Sort()
		}
	}

	for i, p := range s.Pages {
		sect := p.GetSection()
		s.Sections.Add(sect, s.Pages[i])
	}

	for k, _ := range s.Sections {
		s.Sections[k].Sort()
	}

	s.Info.Indexes = s.Indexes.BuildOrderedIndexList()

	if len(s.Pages) == 0 {
		return errors.New(fmt.Sprintf("Unable to build site metadata, no pages found in directory %s", s.c.ContentDir))
	}
	s.Info.LastChange = s.Pages[0].Date

	// populate pages with site metadata
	for _, p := range s.Pages {
		p.Site = s.Info
	}

	return
}

func (s *Site) RenderPages() error {
	for i, _ := range s.Pages {
		content, err := s.RenderThing(s.Pages[i], s.Pages[i].Layout())
		if err != nil {
			return err
		}
		s.Pages[i].RenderedContent = content
	}
	return nil
}

func (s *Site) WritePages() {
	for _, p := range s.Pages {
		s.WritePublic(p.Section + slash + p.OutFile, p.RenderedContent.Bytes())
	}
}

func (s *Site) setOutFile(p *Page) {
	if len(strings.TrimSpace(p.Slug)) > 0 {
		// Use Slug if provided
		if s.c.UglyUrls {
			p.OutFile = strings.TrimSpace(p.Slug + "." + p.Extension)
		} else {
			p.OutFile = strings.TrimSpace(p.Slug + slash + "index.html")
		}
	} else if len(strings.TrimSpace(p.Url)) > 2 {
		// Use Url if provided & Slug missing
		p.OutFile = strings.TrimSpace(p.Url)
	} else {
		// Fall back to filename
		_, t := filepath.Split(p.FileName)
		if s.c.UglyUrls {
			p.OutFile = replaceExtension(strings.TrimSpace(t), p.Extension)
		} else {
			file, _ := fileExt(strings.TrimSpace(t))
			p.OutFile = file + slash + "index." + p.Extension
		}
	}
}

func (s *Site) RenderIndexes() error {
	for singular, plural := range s.c.Indexes {
		for k, o := range s.Indexes[plural] {
			n := s.NewNode()
			n.Title = strings.Title(k)
			url := Urlize(plural + slash + k)
			plink := url
			if s.c.UglyUrls {
				n.Url = url + ".html"
				plink = n.Url
			} else {
				n.Url = url + "/index.html"
			}
			n.Permalink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string(plink)))
			n.RSSlink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string(url+".xml")))
			n.Date = o[0].Date
			n.Data[singular] = o
			n.Data["Pages"] = o
			layout := "indexes" + slash + singular + ".html"
			x, err := s.RenderThing(n, layout)
			if err != nil {
				return err
			}

			var base string
			if s.c.UglyUrls {
				base = plural + "/" + k
			} else {
				base = plural + "/" + k + "/" + "index"
			}

			s.WritePublic(base+".html", x.Bytes())

			if a := s.Tmpl.Lookup("rss.xml"); a != nil {
				// XML Feed
				y := s.NewXMLBuffer()
				if s.c.UglyUrls {
					n.Url = Urlize(plural + "/" + k + ".xml")
				} else {
					n.Url = Urlize(plural + "/" + k + "/" + "index.xml")
				}
				n.Permalink = template.HTML(string(n.Site.BaseUrl) + n.Url)
				s.Tmpl.ExecuteTemplate(y, "rss.xml", n)
				s.WritePublic(base+".xml", y.Bytes())
			}
		}
	}
	return nil
}

func (s *Site) RenderIndexesIndexes() {
	layout := "indexes" + slash + "indexes.html"
	if s.Tmpl.Lookup(layout) != nil {
		for singular, plural := range s.c.Indexes {
			n := s.NewNode()
			n.Title = strings.Title(plural)
			url := Urlize(plural)
			n.Url = url + "/index.html"
			n.Permalink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string(n.Url)))
			n.Data["Singular"] = singular
			n.Data["Plural"] = plural
			n.Data["Index"] = s.Indexes[plural]
			n.Data["OrderedIndex"] = s.Info.Indexes[plural]

			x := s.RenderThing(n, layout)
			s.WritePublic(plural+slash+"index.html", x.Bytes())
		}
	}
}

func (s *Site) RenderLists() error {
	for section, data := range s.Sections {
		n := s.NewNode()
		n.Title = strings.Title(inflect.Pluralize(section))
		n.Url = Urlize(section + "/" + "index.html")
		n.Permalink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string(n.Url)))
		n.RSSlink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string(section+".xml")))
		n.Date = data[0].Date
		n.Data["Pages"] = data
		layout := "indexes" + slash + section + ".html"

		x, err := s.RenderThing(n, layout)
		if err != nil {
			return err
		}
		s.WritePublic(section + slash + "index.html", x.Bytes())

		if a := s.Tmpl.Lookup("rss.xml"); a != nil {
			// XML Feed
			n.Url = Urlize(section + ".xml")
			n.Permalink = template.HTML(string(n.Site.BaseUrl) + n.Url)
			y := s.NewXMLBuffer()
			s.Tmpl.ExecuteTemplate(y, "rss.xml", n)
			s.WritePublic(section + slash + "index.xml", y.Bytes())
		}
	}
	return nil
}

func (s *Site) RenderHomePage() error {
	n := s.NewNode()
	n.Title = n.Site.Title
	n.Url = Urlize(string(n.Site.BaseUrl))
	n.RSSlink = template.HTML(MakePermalink(string(n.Site.BaseUrl), string("index.xml")))
	n.Permalink = template.HTML(string(n.Site.BaseUrl))
	n.Date = s.Pages[0].Date
	if len(s.Pages) < 9 {
		n.Data["Pages"] = s.Pages
	} else {
		n.Data["Pages"] = s.Pages[:9]
	}
	x, err := s.RenderThing(n, "index.html")
	if err != nil {
		return err
	}
	s.WritePublic("index.html", x.Bytes())

	if a := s.Tmpl.Lookup("rss.xml"); a != nil {
		// XML Feed
		n.Url = Urlize("index.xml")
		n.Title = "Recent Content"
		n.Permalink = template.HTML(string(n.Site.BaseUrl) + "index.xml")
		y := s.NewXMLBuffer()
		s.Tmpl.ExecuteTemplate(y, "rss.xml", n)
		s.WritePublic("index.xml", y.Bytes())
	}
	return nil
}

func (s *Site) Stats() {
	fmt.Printf("%d pages created \n", len(s.Pages))
	for _, pl := range s.c.Indexes {
		fmt.Printf("%d %s created\n", len(s.Indexes[pl]), pl)
	}
}

func (s *Site) NewNode() Node {
	var y Node
	y.Data = make(map[string]interface{})
	y.Site = s.Info

	return y
}

func (s *Site) RenderThing(d interface{}, layout string) (*bytes.Buffer, error) {
	buffer := new(bytes.Buffer)
	err := s.Tmpl.ExecuteTemplate(buffer, layout, d)
	return buffer, err
}

func (s *Site) NewXMLBuffer() *bytes.Buffer {
	header := "<?xml version=\"1.0\" encoding=\"utf-8\" standalone=\"yes\" ?>\n"
	return bytes.NewBufferString(header)
}

func (s *Site) WritePublic(path string, content []byte) {

	if s.c.Verbose {
		fmt.Println(path)
	}

	path, filename := filepath.Split(path)

	path = filepath.FromSlash(s.c.GetAbsPath(filepath.Join(s.c.PublishDir, path)))
	err := mkdirIf(path)

	if err != nil {
		fmt.Println(err)
	}

	file, _ := os.Create(filepath.Join(path, filename))
	defer file.Close()

	file.Write(content)
}
