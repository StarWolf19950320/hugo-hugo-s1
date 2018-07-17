// Copyright 2016 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hugolib

import (
	"fmt"
	"path"
	"strings"

	"github.com/gohugoio/hugo/cache"
)

// PageCollections contains the page collections for a site.
type PageCollections struct {
	// Includes only pages of all types, and only pages in the current language.
	Pages Pages

	// Includes all pages in all languages, including the current one.
	// Includes pages of all types.
	AllPages Pages

	// A convenience cache for the traditional index types, taxonomies, home page etc.
	// This is for the current language only.
	indexPages Pages

	// A convenience cache for the regular pages.
	// This is for the current language only.
	RegularPages Pages

	// A convenience cache for the all the regular pages.
	AllRegularPages Pages

	// Includes absolute all pages (of all types), including drafts etc.
	rawAllPages Pages

	// Includes headless bundles, i.e. bundles that produce no output for its content page.
	headlessPages Pages

	pageIndex *cache.Lazy
}

// Get initializes the index if not already done so, then
// looks up the given page ref, returns nil if no value found.
func (c *PageCollections) getFromCache(ref string) (*Page, error) {
	v, found, err := c.pageIndex.Get(ref)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	p := v.(*Page)

	if p != ambiguityFlag {
		return p, nil
	}
	return nil, fmt.Errorf("page reference %q is ambiguous", ref)
}

var ambiguityFlag = &Page{Kind: kindUnknown, title: "ambiguity flag"}

func (c *PageCollections) refreshPageCaches() {
	c.indexPages = c.findPagesByKindNotIn(KindPage, c.Pages)
	c.RegularPages = c.findPagesByKindIn(KindPage, c.Pages)
	c.AllRegularPages = c.findPagesByKindIn(KindPage, c.AllPages)

	var s *Site

	if len(c.Pages) > 0 {
		s = c.Pages[0].s
	}

	indexLoader := func() (map[string]interface{}, error) {
		index := make(map[string]interface{})

		add := func(ref string, p *Page) {
			existing := index[ref]
			if existing == nil {
				index[ref] = p
			} else if existing != ambiguityFlag && existing != p {
				index[ref] = ambiguityFlag
			}
		}

		// Note that we deliberately use the pages from all sites
		// in this index, as we intend to use this in the ref and relref
		// shortcodes.
		for _, pageCollection := range []Pages{c.AllRegularPages, c.headlessPages} {
			for _, p := range pageCollection {
				sourceRef := p.absoluteSourceRef()

				// Allow cross language references by
				// adding the language code as prefix.
				add(path.Join("/"+p.Lang(), sourceRef), p)

				// For pages in the current language.
				if s != nil && p.s == s {
					if sourceRef != "" {
						// index the canonical ref
						// e.g. /section/article.md
						add(sourceRef, p)
					}

					// Ref/Relref supports this potentially ambiguous lookup.
					add(p.Source.LogicalName(), p)

					translationBaseName := p.Source.TranslationBaseName()

					dir, _ := path.Split(sourceRef)
					dir = strings.TrimSuffix(dir, "/")

					if translationBaseName == "index" {
						add(dir, p)
						add(path.Base(dir), p)
					} else {
						add(translationBaseName, p)
					}

					// We need a way to get to the current language version.
					pathWithNoExtensions := path.Join(dir, translationBaseName)
					add(pathWithNoExtensions, p)
				}
			}
		}

		for _, p := range c.indexPages {
			// index the canonical, unambiguous ref for any backing file
			// e.g. /section/_index.md
			sourceRef := p.absoluteSourceRef()
			if sourceRef != "" {
				add(sourceRef, p)
			}

			ref := path.Join(p.sections...)

			// index the canonical, unambiguous virtual ref
			// e.g. /section
			// (this may already have been indexed above)
			add("/"+ref, p)
		}

		return index, nil
	}

	c.pageIndex = cache.NewLazy(indexLoader)
}

func newPageCollections() *PageCollections {
	return &PageCollections{}
}

func newPageCollectionsFromPages(pages Pages) *PageCollections {
	return &PageCollections{rawAllPages: pages}
}

// getPage is the "old style" get page. Deprecated in Hugo 0.45 in favour of
// the "path only" syntax.
// TODO(bep) remove this an rename below once this is all working.
func (c *PageCollections) getPage(typ string, sections ...string) *Page {
	p, _ := c.getPageNew(nil, "/"+path.Join(sections...))
	return p

}

// Ref is either unix-style paths (i.e. callers responsible for
// calling filepath.ToSlash as necessary) or shorthand refs.
func (c *PageCollections) getPageNew(context *Page, ref string) (*Page, error) {

	// Absolute (content root relative) reference.
	if strings.HasPrefix(ref, "/") {
		if p, err := c.getFromCache(ref); err == nil && p != nil {
			return p, nil
		}
	}

	// If there's a page context, try the page relative path.
	if context != nil {
		ppath := path.Join("/", strings.Join(context.sections, "/"), ref)
		if p, err := c.getFromCache(ppath); err == nil && p != nil {
			return p, nil
		}
	}

	if !strings.HasPrefix(ref, "/") {
		// Many people will have "post/foo.md" in their content files.
		if p, err := c.getFromCache("/" + ref); err == nil && p != nil {
			return p, nil
		}
	}

	// Last try.
	ref = strings.TrimPrefix(ref, "/")
	context, err := c.getFromCache(ref)

	if err != nil {
		if context != nil {
			return nil, fmt.Errorf("failed to resolve page relative to page %q: %s", context.absoluteSourceRef(), err)
		}
		return nil, fmt.Errorf("failed to resolve page: %s", err)
	}

	return context, nil
}

func (*PageCollections) findPagesByKindIn(kind string, inPages Pages) Pages {
	var pages Pages
	for _, p := range inPages {
		if p.Kind == kind {
			pages = append(pages, p)
		}
	}
	return pages
}

func (*PageCollections) findFirstPageByKindIn(kind string, inPages Pages) *Page {
	for _, p := range inPages {
		if p.Kind == kind {
			return p
		}
	}
	return nil
}

func (*PageCollections) findPagesByKindNotIn(kind string, inPages Pages) Pages {
	var pages Pages
	for _, p := range inPages {
		if p.Kind != kind {
			pages = append(pages, p)
		}
	}
	return pages
}

func (c *PageCollections) findPagesByKind(kind string) Pages {
	return c.findPagesByKindIn(kind, c.Pages)
}

func (c *PageCollections) addPage(page *Page) {
	c.rawAllPages = append(c.rawAllPages, page)
}

func (c *PageCollections) removePageFilename(filename string) {
	if i := c.rawAllPages.findPagePosByFilename(filename); i >= 0 {
		c.clearResourceCacheForPage(c.rawAllPages[i])
		c.rawAllPages = append(c.rawAllPages[:i], c.rawAllPages[i+1:]...)
	}

}

func (c *PageCollections) removePage(page *Page) {
	if i := c.rawAllPages.findPagePos(page); i >= 0 {
		c.clearResourceCacheForPage(c.rawAllPages[i])
		c.rawAllPages = append(c.rawAllPages[:i], c.rawAllPages[i+1:]...)
	}

}

func (c *PageCollections) findPagesByShortcode(shortcode string) Pages {
	var pages Pages

	for _, p := range c.rawAllPages {
		if p.shortcodeState != nil {
			if _, ok := p.shortcodeState.nameSet[shortcode]; ok {
				pages = append(pages, p)
			}
		}
	}
	return pages
}

func (c *PageCollections) replacePage(page *Page) {
	// will find existing page that matches filepath and remove it
	c.removePage(page)
	c.addPage(page)
}

func (c *PageCollections) clearResourceCacheForPage(page *Page) {
	if len(page.Resources) > 0 {
		first := page.Resources[0]
		dir := path.Dir(first.RelPermalink())
		dir = strings.TrimPrefix(dir, page.LanguagePrefix())
		// This is done to keep the memory usage in check when doing live reloads.
		page.s.ResourceSpec.DeleteCacheByPrefix(dir)
	}
}
