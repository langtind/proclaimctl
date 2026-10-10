// Package plan turns a JSON service plan into a change to a Proclaim
// presentation.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/langtind/proclaimctl/internal/proclaim"
)

// Plan is the desired order of one presentation.
//
// A section left out keeps its current items. A section given as an empty
// list is emptied.
type Plan struct {
	// Presentation is the presentation to change, by id or title.
	Presentation string `json:"presentation"`
	// From creates a new presentation copied from this one instead.
	From string `json:"from,omitempty"`

	Title     string `json:"title,omitempty"`
	Date      string `json:"date,omitempty"`
	StartTime string `json:"start_time,omitempty"`

	// Replace changes slide text on every item with a given title, in any
	// section, such as dates on announcements in the loops: title to old
	// text to new text. Clones show their original, so they follow along.
	Replace map[string]map[string]string `json:"replace,omitempty"`

	PreService  *[]Step `json:"pre_service,omitempty"`
	WarmUp      *[]Step `json:"warm_up,omitempty"`
	Service     *[]Step `json:"service,omitempty"`
	PostService *[]Step `json:"post_service,omitempty"`
}

// Step is one item in the plan. Exactly one of Keep, Reuse, Bible and Video is set.
type Step struct {
	// Keep takes an item from the base presentation, by id or title.
	Keep string `json:"keep,omitempty"`
	// Reuse copies the most recent item with this title from any presentation.
	Reuse string `json:"reuse,omitempty"`
	// Kind limits Reuse to one item kind, such as SongLyrics.
	Kind string `json:"kind,omitempty"`
	// Bible adds an on-screen Bible passage, such as "John 11:1–7".
	Bible string `json:"bible,omitempty"`
	// Video adds a local video file, which Proclaim uploads on its next start.
	Video string `json:"video,omitempty"`
	// Replace changes slide text on the item, old text to new text.
	Replace map[string]string `json:"replace,omitempty"`
}

func (s Step) String() string {
	switch {
	case s.Keep != "":
		return "keep " + s.Keep
	case s.Reuse != "":
		return "reuse " + s.Reuse
	case s.Video != "":
		return "video " + s.Video
	default:
		return "bible " + s.Bible
	}
}

// Parse reads a plan and rejects unknown fields, so a typo fails loudly.
func Parse(r io.Reader) (Plan, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var p Plan
	if err := dec.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("read plan: %w", err)
	}
	return p, p.validate()
}

var (
	datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

func (p Plan) validate() error {
	if p.Presentation == "" && p.From == "" {
		return errors.New(`plan needs "presentation" or "from"`)
	}
	if p.Presentation != "" && p.From != "" {
		return errors.New(`plan takes "presentation" or "from", not both`)
	}
	if p.From != "" && p.Title == "" {
		return errors.New(`a new presentation needs a "title"`)
	}
	if p.Date != "" && !datePattern.MatchString(p.Date) {
		return fmt.Errorf("date %q is not YYYY-MM-DD", p.Date)
	}
	if p.StartTime != "" && !timePattern.MatchString(p.StartTime) {
		return fmt.Errorf("start_time %q is not HH:MM", p.StartTime)
	}
	for section, steps := range p.sections() {
		if steps == nil {
			continue
		}
		for i, s := range *steps {
			set := 0
			for _, v := range []string{s.Keep, s.Reuse, s.Bible, s.Video} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return fmt.Errorf("%s step %d: set exactly one of keep, reuse, bible and video", section, i+1)
			}
			if s.Kind != "" && s.Reuse == "" {
				return fmt.Errorf("%s step %d: kind only applies to reuse", section, i+1)
			}
		}
	}
	return nil
}

func (p Plan) sections() map[proclaim.Section]*[]Step {
	return map[proclaim.Section]*[]Step{
		proclaim.PreService:  p.PreService,
		proclaim.WarmUp:      p.WarmUp,
		proclaim.Service:     p.Service,
		proclaim.PostService: p.PostService,
	}
}

// Source is what a plan is resolved against.
type Source interface {
	FindPresentation(query string) (proclaim.Presentation, error)
	Items(p proclaim.Presentation) ([]proclaim.Item, error)
	LatestItem(title, kind string) (proclaim.Item, error)
	LatestOfKind(kind string) (proclaim.Item, error)
	LatestVideoTemplate() (proclaim.Item, error)
}

// Resolve looks up every step and returns the change to write.
func (p Plan) Resolve(src Source, now time.Time) (proclaim.Change, error) {
	baseQuery := p.Presentation
	if p.From != "" {
		baseQuery = p.From
	}
	base, err := src.FindPresentation(baseQuery)
	if err != nil {
		return proclaim.Change{}, err
	}
	baseItems, err := src.Items(base)
	if err != nil {
		return proclaim.Change{}, err
	}

	change := proclaim.Change{
		Base:      base,
		Title:     p.Title,
		Date:      p.Date,
		StartTime: p.StartTime,
		Items:     map[proclaim.Section][]proclaim.Item{},
	}
	copying := p.From != ""
	if !copying {
		change.Target = base
	}

	used := make([]bool, len(baseItems))
	take := func(it proclaim.Item) proclaim.Item {
		if copying {
			return it.Copy(now)
		}
		return it
	}

	// Sections the plan leaves out keep their items, so those items are not
	// available to a keep elsewhere.
	for i, it := range baseItems {
		if p.sections()[it.Section] == nil {
			used[i] = true
			change.Items[it.Section] = append(change.Items[it.Section], take(it))
		}
	}

	// Steps run section by section, so a keep takes the first unused match
	// in presentation order and repeated titles resolve the way they read.
	for _, section := range proclaim.Sections {
		steps := p.sections()[section]
		if steps == nil {
			continue
		}
		items := []proclaim.Item{}
		for n, s := range *steps {
			it, upload, err := resolveStep(src, s, baseItems, used, take, now)
			if err != nil {
				return proclaim.Change{}, fmt.Errorf("%s step %d (%s): %w", section, n+1, s, err)
			}
			if upload != nil {
				change.Uploads = append(change.Uploads, *upload)
			}
			items = append(items, it)
		}
		change.Items[section] = items
	}
	if err := p.replaceEverywhere(change, now); err != nil {
		return proclaim.Change{}, err
	}
	return change, nil
}

// replaceEverywhere applies the plan's top-level replacements. Every title
// must match an item, and every text must be found on at least one of them.
func (p Plan) replaceEverywhere(change proclaim.Change, now time.Time) error {
	for title, pairs := range p.Replace {
		matched := false
		for old, repl := range pairs {
			found := 0
			for _, section := range proclaim.Sections {
				for i, it := range change.Items[section] {
					if !strings.EqualFold(it.Title, title) || it.Kind == "Clone" {
						continue
					}
					matched = true
					updated, n, err := it.ReplaceText(old, repl, now)
					if err != nil {
						return err
					}
					change.Items[section][i] = updated
					found += n
				}
			}
			if !matched {
				return fmt.Errorf("replace: no item titled %q in the presentation", title)
			}
			if found == 0 {
				return fmt.Errorf("replace: text %q not found on %q", old, title)
			}
		}
	}
	return nil
}

func resolveStep(src Source, s Step, base []proclaim.Item, used []bool,
	take func(proclaim.Item) proclaim.Item, now time.Time) (proclaim.Item, *proclaim.Upload, error) {
	var it proclaim.Item
	var upload *proclaim.Upload
	switch {
	case s.Keep != "":
		i := findKeep(base, used, s.Keep)
		if i < 0 {
			return proclaim.Item{}, nil, fmt.Errorf("no unused item %q in the presentation", s.Keep)
		}
		used[i] = true
		it = take(base[i])
	case s.Reuse != "":
		found, err := src.LatestItem(s.Reuse, s.Kind)
		if err != nil {
			return proclaim.Item{}, nil, err
		}
		it = found.Copy(now)
	case s.Video != "":
		template, err := src.LatestVideoTemplate()
		if err != nil {
			return proclaim.Item{}, nil, err
		}
		var up proclaim.Upload
		it, up, err = template.Copy(now).AsLocalVideo(s.Video, now)
		if err != nil {
			return proclaim.Item{}, nil, err
		}
		upload = &up
	default:
		template, err := src.LatestOfKind("OnScreenBible")
		if err != nil {
			return proclaim.Item{}, nil, err
		}
		it, err = template.Copy(now).WithBibleReference(s.Bible, now)
		if err != nil {
			return proclaim.Item{}, nil, err
		}
	}
	for old, repl := range s.Replace {
		var n int
		var err error
		it, n, err = it.ReplaceText(old, repl, now)
		if err != nil {
			return proclaim.Item{}, nil, err
		}
		if n == 0 {
			return proclaim.Item{}, nil, fmt.Errorf("text %q not found on %q", old, it.Title)
		}
	}
	return it, upload, nil
}

// findKeep returns the first unused item whose id or title matches.
func findKeep(items []proclaim.Item, used []bool, query string) int {
	for i, it := range items {
		if !used[i] && (it.ID == query || strings.EqualFold(it.Title, query)) {
			return i
		}
	}
	return -1
}
