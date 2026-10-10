package proclaim

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Section is one of the four parts of a presentation's order.
type Section string

const (
	PreService  Section = "pre_service"
	WarmUp      Section = "warm_up"
	Service     Section = "service"
	PostService Section = "post_service"
)

// Sections lists the sections in presentation order.
var Sections = []Section{PreService, WarmUp, Service, PostService}

// Presentation is one row of the Presentations table.
type Presentation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Date      string `json:"date"`
	StartTime string `json:"start_time,omitempty"`
	Modified  string `json:"modified"`
	Synced    bool   `json:"synced"`

	// Section boundaries as item indexes, the way Proclaim stores them.
	PreServiceStart  int `json:"-"`
	ServiceStart     int `json:"-"`
	PostServiceStart int `json:"-"`
}

// Item is one service item, the unit you see as a row in Proclaim's order.
type Item struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Section    Section  `json:"section"`
	References []string `json:"references,omitempty"`
	// Uploading is true while a video or image on the item waits for
	// Proclaim to upload it.
	Uploading bool `json:"uploading,omitempty"`

	PresentationID string `json:"-"`
	row            itemRow
}

// itemRow holds every column of a ServiceItems row, so an item can be
// written back or copied without losing anything Proclaim stored.
type itemRow struct {
	ServiceItemID           string
	PresentationID          string
	Kind                    string
	Title                   string
	Content                 sql.NullString
	Notes                   sql.NullString
	Media                   sql.NullString
	ForegroundImages        sql.NullString
	Signals                 sql.NullString
	Modified                string
	CopiedFromServiceItemID sql.NullString
	CreatedFromCopyDate     sql.NullString
}

const presentationColumns = `PresentationId, Title, coalesce(DateGiven, ''), coalesce(StartTime, ''),
	coalesce(ModifiedUtc, ''), SyncState, coalesce(PreServiceStartIndex, 0), coalesce(StartIndex, 0),
	coalesce(PostServiceStartIndex, 0)`

func scanPresentation(row interface{ Scan(...any) error }) (Presentation, error) {
	var p Presentation
	var state int
	err := row.Scan(&p.ID, &p.Title, &p.Date, &p.StartTime, &p.Modified, &state,
		&p.PreServiceStart, &p.ServiceStart, &p.PostServiceStart)
	p.Synced = state == syncSynced
	p.StartTime = strings.TrimSuffix(p.StartTime, ":00")
	return p, err
}

// Presentations lists presentations that are not deleted, newest date first.
func (d *DB) Presentations(limit int) ([]Presentation, error) {
	rows, err := d.sql.Query(`SELECT `+presentationColumns+` FROM Presentations
		WHERE IsDeleted = 0 AND coalesce(IsDigitalSignagePresentation, 0) = 0
		ORDER BY DateGiven DESC, ModifiedUtc DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Presentation
	for rows.Next() {
		p, err := scanPresentation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FindPresentation resolves an id, an exact title, or a unique part of a title.
func (d *DB) FindPresentation(query string) (Presentation, error) {
	all, err := d.Presentations(1 << 20)
	if err != nil {
		return Presentation{}, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var partial []Presentation
	for _, p := range all {
		if p.ID == q || strings.ToLower(p.Title) == q {
			return p, nil
		}
		if strings.Contains(strings.ToLower(p.Title), q) {
			partial = append(partial, p)
		}
	}
	switch len(partial) {
	case 0:
		return Presentation{}, fmt.Errorf("no presentation matches %q", query)
	case 1:
		return partial[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d presentations, use the id:", query, len(partial))
	for _, p := range partial[:min(len(partial), 10)] {
		fmt.Fprintf(&b, "\n  %s  %s  %s", p.ID, p.Date, p.Title)
	}
	return Presentation{}, fmt.Errorf("%s", b.String())
}

const itemColumns = `ServiceItemId, PresentationId, ServiceItemKind, Title, Content, Notes, Media,
	ForegroundImages, Signals, Modified, CopiedFromServiceItemId, CreatedFromCopyDate`

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var r itemRow
	err := row.Scan(&r.ServiceItemID, &r.PresentationID, &r.Kind, &r.Title, &r.Content, &r.Notes,
		&r.Media, &r.ForegroundImages, &r.Signals, &r.Modified, &r.CopiedFromServiceItemID, &r.CreatedFromCopyDate)
	if err != nil {
		return Item{}, err
	}
	return itemFromRow(r), nil
}

func itemFromRow(r itemRow) Item {
	return Item{
		ID:             r.ServiceItemID,
		Kind:           r.Kind,
		Title:          r.Title,
		References:     bibleReferences(r.Content.String),
		Uploading:      pendingUploads(r.Content.String),
		PresentationID: r.PresentationID,
		row:            r,
	}
}

// Items returns a presentation's items in order. Proclaim rewrites all of a
// presentation's rows on every save, so row order is presentation order.
func (d *DB) Items(p Presentation) ([]Item, error) {
	rows, err := d.sql.Query(`SELECT `+itemColumns+` FROM ServiceItems
		WHERE PresentationId = ? ORDER BY RecordId`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Section = p.sectionAt(i)
	}
	return items, nil
}

func (p Presentation) sectionAt(i int) Section {
	switch {
	case i < p.PreServiceStart:
		return PreService
	case i < p.ServiceStart:
		return WarmUp
	case i < p.PostServiceStart:
		return Service
	default:
		return PostService
	}
}

// Use is one place an item title has been used.
type Use struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Uses         int    `json:"uses"`
	LastDate     string `json:"last_date"`
	Presentation string `json:"last_presentation"`
}

// FindItems searches item titles across all presentations. It returns one
// line per kind and title, most recently used first.
func (d *DB) FindItems(query, kind string) ([]Use, error) {
	rows, err := d.sql.Query(`SELECT s.ServiceItemKind, s.Title, coalesce(p.DateGiven, ''), p.Title
		FROM ServiceItems s JOIN Presentations p USING (PresentationId)
		WHERE p.IsDeleted = 0 AND s.ServiceItemKind <> 'Clone'
		ORDER BY p.DateGiven DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	q := strings.ToLower(strings.TrimSpace(query))
	index := map[string]int{}
	var uses []Use
	for rows.Next() {
		var u Use
		if err := rows.Scan(&u.Kind, &u.Title, &u.LastDate, &u.Presentation); err != nil {
			return nil, err
		}
		if kind != "" && !strings.EqualFold(u.Kind, kind) {
			continue
		}
		if !strings.Contains(strings.ToLower(u.Title), q) {
			continue
		}
		key := u.Kind + "\x00" + u.Title
		if i, ok := index[key]; ok {
			uses[i].Uses++
			continue
		}
		u.Uses = 1
		index[key] = len(uses)
		uses = append(uses, u)
	}
	return uses, rows.Err()
}

// LatestItem returns the most recent use of an item with this exact title,
// optionally limited to one kind. Title matching ignores case.
func (d *DB) LatestItem(title, kind string) (Item, error) {
	rows, err := d.sql.Query(`SELECT ` + prefixed("s.", itemColumns) + `
		FROM ServiceItems s JOIN Presentations p USING (PresentationId)
		WHERE p.IsDeleted = 0 AND s.ServiceItemKind <> 'Clone'
		ORDER BY p.DateGiven DESC, s.RecordId DESC`)
	if err != nil {
		return Item{}, err
	}
	defer rows.Close()
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return Item{}, err
		}
		if strings.EqualFold(it.Title, title) && (kind == "" || strings.EqualFold(it.Kind, kind)) {
			return it, nil
		}
	}
	if err := rows.Err(); err != nil {
		return Item{}, err
	}
	if kind != "" {
		return Item{}, fmt.Errorf("no %s item titled %q in any presentation", kind, title)
	}
	return Item{}, fmt.Errorf("no item titled %q in any presentation", title)
}

// LatestOfKind returns the most recent item of a kind, used as a template.
func (d *DB) LatestOfKind(kind string) (Item, error) {
	row := d.sql.QueryRow(`SELECT `+prefixed("s.", itemColumns)+`
		FROM ServiceItems s JOIN Presentations p USING (PresentationId)
		WHERE p.IsDeleted = 0 AND s.ServiceItemKind = ?
		ORDER BY p.DateGiven DESC, s.RecordId DESC LIMIT 1`, kind)
	it, err := scanItem(row)
	if err == sql.ErrNoRows {
		return Item{}, fmt.Errorf("no %s item in any presentation to use as a template", kind)
	}
	return it, err
}

func prefixed(prefix, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// bibleReferences reads the passages of an OnScreenBible item.
func bibleReferences(content string) []string {
	if !strings.Contains(content, `"References"`) {
		return nil
	}
	var c map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &c) != nil {
		return nil
	}
	var encoded string
	if json.Unmarshal(c["References"], &encoded) != nil {
		return nil
	}
	var refs []struct {
		Reference string `json:"reference"`
	}
	if json.Unmarshal([]byte(encoded), &refs) != nil {
		return nil
	}
	var out []string
	for _, r := range refs {
		if r.Reference != "" && !slices.Contains(out, r.Reference) {
			out = append(out, r.Reference)
		}
	}
	return out
}

// pendingUploads reports whether the item lists upload jobs that have not
// completed. Proclaim removes a job from UploadJobIds when it completes.
func pendingUploads(content string) bool {
	if !strings.Contains(content, `"UploadJobIds"`) {
		return false
	}
	var c map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &c) != nil {
		return false
	}
	var encoded string
	if json.Unmarshal(c["UploadJobIds"], &encoded) != nil {
		return false
	}
	var ids []string
	return json.Unmarshal([]byte(encoded), &ids) == nil && len(ids) > 0
}
