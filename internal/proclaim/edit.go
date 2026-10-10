package proclaim

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NewID returns an id in Proclaim's format: 32 lowercase hex digits.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b)
}

// Copy returns the item under a new id, ready to be placed in another
// presentation. Proclaim does the same when you duplicate an item.
func (it Item) Copy(now time.Time) Item {
	r := it.row
	r.ServiceItemID = NewID()
	r.Modified = now.UTC().Format(time.RFC3339)
	r.CopiedFromServiceItemID = sql.NullString{}
	r.CreatedFromCopyDate = sql.NullString{}
	return itemFromRow(r)
}

// WithBibleReference points an OnScreenBible item at a new passage. The
// cached passage text is cleared, and Proclaim fetches the text from the
// licensed Bible the next time the item is shown.
func (it Item) WithBibleReference(reference string, now time.Time) (Item, error) {
	if it.Kind != "OnScreenBible" {
		return Item{}, fmt.Errorf("%q is a %s item, not an OnScreenBible item", it.Title, it.Kind)
	}
	err := it.editContent(now, func(c map[string]json.RawMessage) error {
		refs, err := json.Marshal([]map[string]string{
			{"reference": reference, "searchText": reference, "syncStatus": "noError"},
			{"syncStatus": "noError"},
		})
		if err != nil {
			return err
		}
		if err := setString(c, "References", string(refs)); err != nil {
			return err
		}
		cache := map[string]json.RawMessage{}
		if raw, ok := c["AnimationCache"]; ok {
			var encoded string
			if json.Unmarshal(raw, &encoded) == nil {
				_ = json.Unmarshal([]byte(encoded), &cache)
			}
		}
		cache["referenceCache"] = json.RawMessage("{}")
		encoded, err := json.Marshal(cache)
		if err != nil {
			return err
		}
		return setString(c, "AnimationCache", string(encoded))
	})
	if err != nil {
		return Item{}, err
	}
	it.References = []string{reference}
	return it, nil
}

// ReplaceText replaces every occurrence of old with new in the item's slide
// text and title. It returns how many places changed.
func (it Item) ReplaceText(old, new string, now time.Time) (Item, int, error) {
	if old == "" {
		return it, 0, errors.New("the text to replace must not be empty")
	}
	// Slide text sits in JSON strings, some of them encoded twice.
	count := 0
	content := it.row.Content.String
	for _, enc := range []func(string) string{jsonInner, func(s string) string { return jsonInner(jsonInner(s)) }} {
		o, n := enc(old), enc(new)
		count += strings.Count(content, o)
		content = strings.ReplaceAll(content, o, n)
		if o == old {
			break
		}
	}
	if count == 0 {
		return it, 0, nil
	}
	it.row.Content.String = content
	it.row.Title = strings.ReplaceAll(it.row.Title, old, new)
	it.Title = it.row.Title
	if err := it.editContent(now, func(map[string]json.RawMessage) error { return nil }); err != nil {
		return Item{}, 0, err
	}
	return it, count, nil
}

// jsonInner escapes s the way it appears inside a JSON string.
func jsonInner(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// editContent changes the item's Content JSON and asks Proclaim to render
// its slides again.
func (it *Item) editContent(now time.Time, edit func(map[string]json.RawMessage) error) error {
	c := map[string]json.RawMessage{}
	if it.row.Content.Valid && it.row.Content.String != "" {
		if err := json.Unmarshal([]byte(it.row.Content.String), &c); err != nil {
			return fmt.Errorf("read content of %q: %w", it.Title, err)
		}
	}
	if err := edit(c); err != nil {
		return err
	}
	if err := setString(c, "RenderVersion", now.UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
		return err
	}
	out, err := json.Marshal(c)
	if err != nil {
		return err
	}
	it.row.Content = sql.NullString{String: string(out), Valid: true}
	it.row.Modified = now.UTC().Format(time.RFC3339)
	return nil
}

func setString(c map[string]json.RawMessage, key, value string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c[key] = raw
	return nil
}

// Change describes the new state of one presentation.
type Change struct {
	// Target is the presentation to overwrite. Empty Target.ID with a
	// non-empty Base creates a new presentation copied from Base.
	Target Presentation
	Base   Presentation

	Title     string
	Date      string
	StartTime string

	Items map[Section][]Item

	// Uploads are videos for Proclaim to upload on its next start.
	Uploads []Upload
}

// Apply writes a change in one transaction and marks the presentation for
// upload. It returns the id of the presentation it wrote.
func (d *DB) Apply(c Change, now time.Time) (string, error) {
	if len(c.Uploads) > 0 {
		if _, err := d.sql.Exec(`ATTACH DATABASE ? AS uploads`, UploadDBPath(d.path)); err != nil {
			return "", fmt.Errorf("open upload queue: %w", err)
		}
		defer func() { _, _ = d.sql.Exec(`DETACH DATABASE uploads`) }()
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	id := c.Target.ID
	if id == "" {
		id = NewID()
		if err := copyPresentationRow(tx, c.Base.ID, id); err != nil {
			return "", err
		}
	}

	var order []Item
	bounds := make([]int, 0, len(Sections))
	for _, s := range Sections {
		order = append(order, c.Items[s]...)
		bounds = append(bounds, len(order))
	}

	seen := map[string]bool{}
	for _, it := range order {
		if seen[it.ID] {
			return "", fmt.Errorf("item %q (%s) appears twice", it.Title, it.ID)
		}
		seen[it.ID] = true
	}

	if _, err := tx.Exec(`DELETE FROM ServiceItems WHERE PresentationId = ?`, id); err != nil {
		return "", err
	}
	for _, it := range order {
		r := it.row
		if _, err := tx.Exec(`INSERT INTO ServiceItems (`+itemColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ServiceItemID, id, r.Kind, r.Title, r.Content, r.Notes, r.Media, r.ForegroundImages,
			r.Signals, r.Modified, r.CopiedFromServiceItemID, r.CreatedFromCopyDate); err != nil {
			return "", fmt.Errorf("write item %q: %w", r.Title, err)
		}
	}

	if len(c.Uploads) > 0 {
		var groupID int64
		if err := tx.QueryRow(`SELECT GroupId FROM Presentations WHERE PresentationId = ?`, id).Scan(&groupID); err != nil {
			return "", fmt.Errorf("read the presentation's group: %w", err)
		}
		if err := queueUploads(tx, c.Uploads, groupID, now); err != nil {
			return "", err
		}
	}

	content, err := presentationContent(tx, id, c.StartTime)
	if err != nil {
		return "", err
	}
	set := []string{
		"PreServiceStartIndex = ?", "StartIndex = ?", "PostServiceStartIndex = ?",
		"Content = ?", "Modified = ?", "ModifiedUtc = ?", "SyncState = ?",
	}
	args := []any{bounds[0], bounds[1], bounds[2], content,
		now.Local().Format(time.RFC3339), now.UTC().Format(time.RFC3339), syncChanged}
	if c.Title != "" {
		set, args = append(set, "Title = ?"), append(args, c.Title)
	}
	if c.Date != "" {
		set, args = append(set, "DateGiven = ?"), append(args, c.Date)
	}
	if c.StartTime != "" {
		set, args = append(set, "StartTime = ?"), append(args, c.StartTime+":00")
	}
	args = append(args, id)
	if _, err := tx.Exec(`UPDATE Presentations SET `+strings.Join(set, ", ")+` WHERE PresentationId = ?`, args...); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// copyPresentationRow copies every column of a presentation into a new row
// that Proclaim will upload as a new presentation.
func copyPresentationRow(tx *sql.Tx, from, to string) error {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info('Presentations')`)
	if err != nil {
		return err
	}
	var cols, values []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		cols = append(cols, name)
		switch name {
		case "PresentationId":
			values = append(values, "?")
		case "SyncRevision", "LastViewed", "LastViewedUtc":
			values = append(values, "NULL")
		default:
			values = append(values, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT INTO Presentations (`+strings.Join(cols, ", ")+`) SELECT `+
		strings.Join(values, ", ")+` FROM Presentations WHERE PresentationId = ?`, to, from)
	if err != nil {
		return fmt.Errorf("copy presentation: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("copy presentation: %s not found", from)
	}
	return nil
}

// presentationContent returns the presentation's settings JSON with the
// start time Proclaim shows next to the Service heading.
func presentationContent(tx *sql.Tx, id, startTime string) (string, error) {
	var content sql.NullString
	if err := tx.QueryRow(`SELECT Content FROM Presentations WHERE PresentationId = ?`, id).Scan(&content); err != nil {
		return "", err
	}
	if startTime == "" || !content.Valid {
		return content.String, nil
	}
	c := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(content.String), &c); err != nil {
		return "", fmt.Errorf("read presentation settings: %w", err)
	}
	times, _ := json.Marshal([]string{startTime + ":00"})
	if err := setString(c, "PresentationStartTimes", string(times)); err != nil {
		return "", err
	}
	if err := setString(c, "IsStartTimeOverridden", "true"); err != nil {
		return "", err
	}
	out, err := json.Marshal(c)
	return string(out), err
}

// Delete marks a presentation as deleted, the way Proclaim does it. Proclaim
// uploads the deletion on its next start, and the presentation disappears
// from every computer in the group.
func (d *DB) Delete(p Presentation, now time.Time) error {
	res, err := d.sql.Exec(`UPDATE Presentations SET Modified = ?, ModifiedUtc = ?, IsDeleted = 1, SyncState = ?
		WHERE PresentationId = ? AND IsDeleted = 0`,
		now.Local().Format(time.RFC3339), now.UTC().Format(time.RFC3339), syncChanged, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("presentation %s not found", p.ID)
	}
	return nil
}
