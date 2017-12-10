package scrape

import (
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/segmentio/ksuid"
	"github.com/slotix/dataflowkit/extract"
	"github.com/slotix/dataflowkit/paginate"
	"github.com/temoto/robotstxt"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "scrape: ", log.Lshortfile)
}

var (
	ErrNoParts = errors.New("no pieces in the config")
)

// The DividePageFunc type is used to extract a page's blocks during a scrape.
// For more information, please see the documentation on the ScrapeConfig type.
type DividePageFunc func(*goquery.Selection) []*goquery.Selection

// A Part represents a given chunk of data that is to be extracted from every
// block in each page of a scrape.
type Part struct {
	// The name of this part.  Required, and will be used to aggregate results.
	Name string

	// A sub-selector within the given block to process.  Pass in "." to use
	// the root block's selector with no modification.
	Selector string
	// TODO(andrew-d): Consider making this an interface too.

	// Extractor contains the logic on how to extract some results from the
	// selector that is provided to this Piece.
	Extractor extract.Extractor
	Details   *Scraper
}

//Scraper struct consolidates settings for scraping task.
type Scraper struct {
	// Paginator is the Paginator to use for this current scrape.
	//
	// If Paginator is nil, then no pagination is performed and it is assumed that
	// the initial URL is the only page.
	Paginator paginate.Paginator

	// DividePage splits a page into individual 'blocks'.  When scraping, we treat
	// each page as if it contains some number of 'blocks', each of which can be
	// further subdivided into what actually needs to be extracted.
	//
	// If the DividePage function is nil, then no division is performed and the
	// page is assumed to contain a single block containing the entire <body>
	// tag.
	DividePage DividePageFunc

	// Parts contains the list of data that is extracted for each block.  For
	// every block that is the result of the DividePage function (above), all of
	// the Parts entries receives the selector representing the block, and can
	// return a result.  If the returned result is nil, then the Part is
	// considered not to exist in this block, and is not included.
	//
	// Note: if a Part's Extractor returns an error, it results in the scrape
	// being aborted - this can be useful if you need to ensure that a given Part
	// is required, for example.
	Parts []Part
	//Opts contains options that are used during the progress of a
	// scrape.
	Opts ScrapeOptions
}

// Results describes the results of a scrape.  It contains a list of all
// pages (URLs) visited during the process, along with all results generated
// from each Part in each page.
type Results struct {
	// All Visited contain a map[url]error if any during this scrape.
	//  Always contains at least one element - the initial URL.
	Visited map[string]error

	// The results from each Part of each page.  Essentially, the top-level array
	// is for each page, the second-level array is for each block in a page, and
	// the final map[string]interface{} is the mapping of Part.Name to results.
	Results [][]map[string]interface{}
}

// First returns the first set of results - i.e. the results from the first
// block on the first page.
// This function can return nil if there were no blocks found on the first page
// of the scrape.
func (r *Results) First() map[string]interface{} {
	if len(r.Results[0]) == 0 {
		return nil
	}

	return r.Results[0][0]
}

// AllBlocks returns a single list of results from every block on all pages.
// This function will always return a list, even if no blocks were found.
func (r *Results) AllBlocks() []map[string]interface{} {
	ret := []map[string]interface{}{}

	for _, page := range r.Results {
		for _, block := range page {
			ret = append(ret, block)
		}
	}

	return ret
}

type Session struct {
	Robots  *robotstxt.RobotsData
	Cookies string
}
type Task struct {
	ID      string
	Scraper *Scraper
	Session
	//	Err     []error
	Status string
	Results
}

func NewTask(p Payload) (task *Task, err error) {
	//config, err := p.PayloadToScrapeConfig()
	//if err != nil {
	//	return nil, err
	//}
	scraper, err := NewScraper(p)
	if err != nil {
		return nil, err
	}
	//https://blog.kowalczyk.info/article/JyRZ/generating-good-random-and-unique-ids-in-go.html
	id := ksuid.New()

	task = &Task{
		ID:      id.String(),
		Scraper: scraper,
	}
	return task, nil
}

//KSUID stores the timestamp portion in ID. So we can retrieve it from Task object as a Time object
func (t Task) StartTime() (*time.Time, error) {
	id, err := ksuid.Parse(t.ID)
	if err != nil {
		return nil, err
	}
	idTime := id.Time()
	return &idTime, nil
}

// Create a new scraper with the provided configuration.
func NewScraper(p Payload) (*Scraper, error) {
	parts := []Part{}
	selectors := []string{}
	for _, f := range p.Fields {
		params := make(map[string]interface{})
		if f.Extractor.Params != nil {
			params = f.Extractor.Params.(map[string]interface{})
		}
		switch eType := f.Extractor.Type; eType {

		//For Link type Two pieces as pair Text and Attr{Attr:"href"} extractors are added.
		case "link":
			l := &extract.Link{Href: extract.Attr{Attr: "href"}}
			if params != nil {
				err := FillStruct(params, l)
				if err != nil {
					logger.Println(err)
				}
			}
			parts = append(parts, Part{
				Name:      f.Name + "_text",
				Selector:  f.Selector,
				Extractor: l.Text,
			}, Part{
				Name:      f.Name + "_link",
				Selector:  f.Selector,
				Extractor: l.Href,
			})
			//Add selector just one time for link type
			selectors = append(selectors, f.Selector)

		//For image type by default Two pieces with different Attr="src" and Attr="alt" extractors will be added for field selector.
		case "image":
			i := &extract.Image{Src: extract.Attr{Attr: "src"},
				Alt: extract.Attr{Attr: "alt"}}
			if params != nil {
				err := FillStruct(params, i)
				if err != nil {
					logger.Println(err)
				}
			}
			parts = append(parts, Part{
				Name:      f.Name + "_src",
				Selector:  f.Selector,
				Extractor: i.Src,
			}, Part{
				Name:      f.Name + "_alt",
				Selector:  f.Selector,
				Extractor: i.Alt,
			})
			//Add selector just one time for image type
			selectors = append(selectors, f.Selector)

		default:
			var e extract.Extractor
			switch eType {
			case "const":
				//	c := &extract.Const{Val: params["value"]}
				//	e = c
				e = &extract.Const{}
			case "count":
				e = &extract.Count{}
			case "text":
				e = &extract.Text{}
			case "html":
				e = &extract.Html{}
			case "outerHtml":
				e = &extract.OuterHtml{}
			case "attr":
				e = &extract.Attr{}
			case "regex":
				r := &extract.Regex{}
				regExp := params["regexp"]
				r.Regex = regexp.MustCompile(regExp.(string))
				e = r
			}

			if params != nil {
				err := FillStruct(params, e)
				if err != nil {
					logger.Println(err)
				}
			}
			parts = append(parts, Part{
				Name:      f.Name,
				Selector:  f.Selector,
				Extractor: e,
			})
			selectors = append(selectors, f.Selector)
			//	names = append(names, f.Name)
		}
	}

	// Validate config
	if len(parts) == 0 {
		return nil, ErrNoParts
	}

	seenNames := map[string]struct{}{}
	for i, part := range parts {
		if len(part.Name) == 0 {
			return nil, fmt.Errorf("no name provided for part %d", i)
		}
		if _, seen := seenNames[part.Name]; seen {
			return nil, fmt.Errorf("part %s has a duplicate name", i)
		}
		seenNames[part.Name] = struct{}{}

		if len(part.Selector) == 0 {
			return nil, fmt.Errorf("no selector provided for part %d", i)
		}
	}

	var paginator paginate.Paginator
	if p.Paginator == nil {
		paginator = &dummyPaginator{}
	} else {
		paginator = paginate.BySelector(p.Paginator.Selector, p.Paginator.Attribute)
	}

	//TODO: need to test the case when there are no selectors found in payload.
	var dividePageFunc DividePageFunc
	if len(selectors) == 0 {
		dividePageFunc = DividePageBySelector("body")
	} else {
		dividePageFunc = DividePageByIntersection(selectors)
	}

	scraper := &Scraper{
		DividePage: dividePageFunc,
		Parts:      parts,
		Paginator:  paginator,
		Opts: ScrapeOptions{
			MaxPages:            p.Paginator.MaxPages,
			Format:              p.Format,
			PaginateResults:     *p.PaginateResults,
			FetchDelay:          p.FetchDelay,
			RandomizeFetchDelay: *p.RandomizeFetchDelay,
			RetryTimes:          p.RetryTimes,
		},
	}

	// All set!
	//ret := &Scraper{
	//	Config: c,
	//}
	return scraper, nil
}

func (s Scraper) PartNames() []string {
	names := []string{}
	for _, part := range s.Parts {
		names = append(names, part.Name)
	}
	return names
}

/*
// ScrapeWithDefaultOpts Scrape a given URL with default options.  See 'Scrape' for more
// information.
func (s *Scraper) ScrapeWithDefaultOpts(req interface{}) (*ScrapeResults, error) {
	return s.Scrape(req, DefaultOptions)
}

// Actually start scraping at the given URL.
//
// Note that, while this function and the Scraper in general are safe for use
// from multiple goroutines, making multiple requests in parallel can cause
// strange behaviour - e.g. overwriting cookies in the underlying http.Client.
// Please be careful when running multiple scrapes at a time, unless you know
// that it's safe.

func (s *Scraper) Scrape(req interface{}, opts ScrapeOptions) (*ScrapeResults, error) {

	var url string
	switch v := req.(type) {
	case HttpClientFetcherRequest:
		url = v.URL
	case splash.Request:
		url = v.URL
	}
	logger.Println(req)
	//rt := fmt.Sprintf("%T\n", req)
	//logger.Println(r)

	if len(url) == 0 {
		return nil, errors.New("no URL provided")
	}

	// Prepare the fetcher.
	err := s.Config.Fetcher.Prepare()
	if err != nil {
		return nil, err
	}

	res := &ScrapeResults{
		URLs:    []string{},
		Results: [][]map[string]interface{}{},
	}

	var numPages int
	for {
		// Repeat until we don't have any more URLs, or until we hit our page limit.
		if len(url) == 0 || (opts.MaxPages > 0 && numPages >= opts.MaxPages) {
			break
		}

		r, err := s.Config.Fetcher.Fetch(req)

		if err != nil {
			return nil, err
		}

		var resp io.ReadCloser
		switch req.(type) {
		case HttpClientFetcherRequest:
			resp = r.(io.ReadCloser)
		case splash.Request:
			if sResponse, ok := r.(*splash.Response); ok {
				resp, err = sResponse.GetContent()
				if err != nil {
					logger.Println(err)
				}
			}
		}

		// Create a goquery document.
		doc, err := goquery.NewDocumentFromReader(resp)
		resp.Close()
		if err != nil {
			return nil, err
		}
		res.URLs = append(res.URLs, url)
		results := []map[string]interface{}{}

		// Divide this page into blocks
		for _, block := range s.Config.DividePage(doc.Selection) {
			blockResults := map[string]interface{}{}

			// Process each piece of this block
			for _, piece := range s.Config.Pieces {
				//logger.Println(piece)
				sel := block
				if piece.Selector != "." {
					sel = sel.Find(piece.Selector)
				}

				pieceResults, err := piece.Extractor.Extract(sel)
				//logger.Println(attrOrDataValue(sel))
				if err != nil {
					return nil, err
				}

				// A nil response from an extractor means that we don't even include it in
				// the results.
				//if pieceResults == nil || pieceResults == "" {
				if pieceResults == nil {
					continue
				}

				blockResults[piece.Name] = pieceResults
			}
			if len(blockResults) > 0 {
				// Append the results from this block.
				results = append(results, blockResults)
			}
		}

		// Append the results from this page.
		res.Results = append(res.Results, results)

		numPages++

		// Get the next page.
		url, err = s.Config.Paginator.NextPage(url, doc.Selection)
		if err != nil {
			return nil, err
		}

		switch req.(type) {
		case HttpClientFetcherRequest:
			req = HttpClientFetcherRequest{URL: url}
		case splash.Request:
			//every time when getting a response the next request will be filled with updated cookie information
			if sResponse, ok := r.(*splash.Response); ok {
				setCookie, err := sResponse.SetCookieToRequest()
				if err != nil {
					//return nil, err
					logger.Println(err)
				}
				req = splash.Request{URL: url, Cookies: setCookie}
			}
		}
	}

	// All good!
	return res, nil
}
*/
