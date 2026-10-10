package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/langtind/proclaimctl/internal/proclaim"
)

var sectionNames = map[proclaim.Section]string{
	proclaim.PreService:  "Pre-service loop",
	proclaim.WarmUp:      "Warm-up",
	proclaim.Service:     "Service",
	proclaim.PostService: "Post-service loop",
}

// renderPresentations prints Markdown, which reads well both in a terminal
// and when an agent relays it into a chat.
func renderPresentations(w io.Writer, list []proclaim.Presentation) {
	fmt.Fprintf(w, "# Presentations (%d)\n\n", len(list))
	for _, p := range list {
		fmt.Fprintf(w, "- %s - %s", p.Date, p.Title)
		if !p.Synced {
			fmt.Fprint(w, " (not uploaded yet)")
		}
		fmt.Fprintf(w, "\n  id: %s\n", p.ID)
	}
}

func renderPresentation(w io.Writer, p proclaim.Presentation, items []proclaim.Item) {
	fmt.Fprintf(w, "# %s\n", p.Title)
	fmt.Fprintf(w, "date: %s", p.Date)
	if p.StartTime != "" {
		fmt.Fprintf(w, ", service starts %s", p.StartTime)
	}
	fmt.Fprintf(w, "\nid: %s\n", p.ID)
	if !p.Synced {
		fmt.Fprintln(w, "Changed locally, not uploaded yet.")
	}
	bySection := map[proclaim.Section][]proclaim.Item{}
	for _, it := range items {
		bySection[it.Section] = append(bySection[it.Section], it)
	}
	renderSections(w, bySection, true)
}

func renderSections(w io.Writer, bySection map[proclaim.Section][]proclaim.Item, withIDs bool) {
	n := 0
	for _, s := range proclaim.Sections {
		items := bySection[s]
		fmt.Fprintf(w, "\n## %s (%d)\n\n", sectionNames[s], len(items))
		for _, it := range items {
			n++
			fmt.Fprintf(w, "%d. %s\n", n, describe(it))
			if withIDs {
				fmt.Fprintf(w, "   id: %s\n", it.ID)
			}
		}
	}
}

func describe(it proclaim.Item) string {
	s := it.Title + " [" + it.Kind + "]"
	if len(it.References) > 0 {
		s += " " + strings.Join(it.References, "; ")
	}
	return s
}

func renderUses(w io.Writer, query string, uses []proclaim.Use) {
	fmt.Fprintf(w, "# %q (%d)\n\n", query, len(uses))
	for _, u := range uses {
		fmt.Fprintf(w, "- %s [%s] - used %d times, last %s in %q\n", u.Title, u.Kind, u.Uses, u.LastDate, u.Presentation)
	}
}

func renderChange(w io.Writer, c proclaim.Change) {
	title := c.Title
	if title == "" {
		title = c.Target.Title
	}
	if c.Target.ID == "" {
		fmt.Fprintf(w, "# New presentation %q, copied from %q\n", title, c.Base.Title)
	} else {
		fmt.Fprintf(w, "# %s\n", title)
	}
	if c.Date != "" {
		fmt.Fprintf(w, "date: %s\n", c.Date)
	}
	if c.StartTime != "" {
		fmt.Fprintf(w, "service starts: %s\n", c.StartTime)
	}
	renderSections(w, c.Items, false)
}
