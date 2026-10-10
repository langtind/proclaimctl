package proclaim

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// localMediaID is the media id Proclaim gives a file that is not uploaded
// yet. The item's localfile key says which file it is.
const localMediaID = "fcfb311c-1b42-4eae-b4b6-7f0d2b4b4860"

// Upload is a video that Proclaim uploads to the church's media library the
// next time it starts. When the upload completes, Proclaim swaps the local
// file on the item for the uploaded media and syncs the presentation again.
type Upload struct {
	JobID         string
	FileID        string
	ServiceItemID string
	Path          string
	Name          string
	Size          int64
}

// LatestVideoTemplate returns a recent Video item to copy settings from. A
// countdown video has countdown settings, so it is the last resort.
func (d *DB) LatestVideoTemplate() (Item, error) {
	rows, err := d.sql.Query(`SELECT ` + prefixed("s.", itemColumns) + `
		FROM ServiceItems s JOIN Presentations p USING (PresentationId)
		WHERE p.IsDeleted = 0 AND s.ServiceItemKind = 'Video'
		ORDER BY p.DateGiven DESC, s.RecordId DESC`)
	if err != nil {
		return Item{}, err
	}
	defer rows.Close()
	var fallback *Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return Item{}, err
		}
		if !strings.Contains(strings.ToLower(it.Title), "countdown") {
			return it, nil
		}
		if fallback == nil {
			fallback = &it
		}
	}
	if err := rows.Err(); err != nil {
		return Item{}, err
	}
	if fallback == nil {
		return Item{}, fmt.Errorf("no Video item in any presentation to use as a template")
	}
	return *fallback, nil
}

// AsLocalVideo turns a copied Video item into one that plays a local file
// and returns the upload that replaces the file with uploaded media.
func (it Item) AsLocalVideo(path string, now time.Time) (Item, Upload, error) {
	if it.Kind != "Video" {
		return Item{}, Upload{}, fmt.Errorf("%q is a %s item, not a Video item", it.Title, it.Kind)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return Item{}, Upload{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Item{}, Upload{}, err
	}
	if info.IsDir() {
		return Item{}, Upload{}, fmt.Errorf("%s is a folder, not a video file", path)
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	up := Upload{
		JobID:         NewID(),
		FileID:        NewID(),
		ServiceItemID: it.ID,
		Path:          path,
		Name:          name,
		Size:          info.Size(),
	}

	err = it.editContent(now, func(c map[string]json.RawMessage) error {
		for key := range c {
			if strings.HasPrefix(key, "localfile:") {
				delete(c, key)
			}
		}
		jobs, err := json.Marshal([]string{dashed(up.JobID)})
		if err != nil {
			return err
		}
		if err := setString(c, "UploadJobIds", string(jobs)); err != nil {
			return err
		}
		local, err := json.Marshal(map[string]any{
			"uploadJobFileId": dashed(up.FileID),
			"name":            name,
			"path":            path,
			"videoDuration":   "00:00:00",
			"loopVideo":       false,
		})
		if err != nil {
			return err
		}
		if err := setString(c, "localfile:0:"+name, string(local)); err != nil {
			return err
		}
		return setString(c, "IsTitleOverridden2", "false")
	})
	if err != nil {
		return Item{}, Upload{}, err
	}
	media, _ := json.Marshal([]string{localMediaID})
	it.row.Media = sql.NullString{String: string(media), Valid: true}
	it.row.Title = name
	it.Title = name
	return it, up, nil
}

// dashed formats a 32 digit id as a GUID with dashes, the way Proclaim
// writes ids inside JSON.
func dashed(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

// properties is the job file JSON Proclaim reads back at startup. With
// needsTranscode the server makes the preview images, so none are needed here.
func (u Upload) properties(now time.Time) (string, error) {
	b, err := json.Marshal(map[string]any{
		"originalPath":   u.Path,
		"needsTranscode": true,
		"videoOffset":    11.0,
		"videoDuration":  "00:00:00",
		"contentLength":  u.Size,
		"videoInfo":      map[string]any{},
		"mediaInfos": []any{map[string]any{
			"source":   "Upload",
			"category": "Motion",
			"about": map[string]any{
				"version": now.UTC().Format(time.RFC3339),
				"name":    u.Name,
			},
			"formats": map[string]any{"Custom": map[string]any{"textFields": []any{}, "videoInfo": map[string]any{}}},
		}},
	})
	return string(b), err
}

// UploadDBPath is UploadManager.db next to the presentation database.
func UploadDBPath(presentationDB string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(presentationDB)), "UploadManager", "UploadManager.db")
}

// queueUploads writes the uploads into the attached UploadManager.db inside
// the same transaction, so both databases change together or not at all.
func queueUploads(tx *sql.Tx, uploads []Upload, groupID int64, now time.Time) error {
	for _, u := range uploads {
		props, err := u.properties(now)
		if err != nil {
			return err
		}
		group := NewID()
		if _, err := tx.Exec(`INSERT INTO uploads.UploadJobs (UploadJobId, State, GroupId, ServiceItemId, Name)
			VALUES (?, 0, ?, ?, NULL)`, u.JobID, groupID, u.ServiceItemID); err != nil {
			return fmt.Errorf("queue upload of %s: %w", u.Path, err)
		}
		if _, err := tx.Exec(`INSERT INTO uploads.UploadFileGroups (UploadFileGroupId, UploadJobId, State, Kind)
			VALUES (?, ?, 0, 0)`, group, u.JobID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO uploads.UploadFiles (UploadId, UploadFileGroupId, Kind, State, Properties)
			VALUES (?, ?, 'Video', 0, ?)`, u.FileID, group, props); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ServiceItemUploadJobs (ServiceItemId, UploadJobId) VALUES (?, ?)`,
			u.ServiceItemID, u.JobID); err != nil {
			return err
		}
	}
	return nil
}
