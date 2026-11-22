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

// itemBase is the content Proclaim writes for every new item, copied from
// what Proclaim 4.22 stored when slides were dropped into a presentation.
var itemBase = map[string]string{
	"Audio":                    `{"setting":0,"continueSetting":0,"audioTracks":[],"uploadJobIds":[],"isDividerItem":false,"overrideUseAudio":false,"localFileContents":{}}`,
	"AudioShuffle":             "false",
	"AudioAutoPlay":            "true",
	"CountdownTime":            "0001-01-01T00:00:00+00:00",
	"ShowCountdownClock":       "false",
	"AdvanceWhenCountdownEnds": "Hide",
	"AutoAdvance":              "false",
	"Repeat":                   "false",
	"AutoAdvanceTime":          "PT3S",
	"HasTargetDuration":        "false",
	"TargetDuration":           "PT1M",
	"Shuffle":                  "false",
	"ShuffleIndexes":           "[]",
	"UseCustomTransition":      "false",
	"IsDigitalSignageDisplayStartDateEnabled": "false",
	"IsDigitalSignageDisplayStopDateEnabled":  "false",
	"IsKeepWhenExpiredEnabled":                "false",
	"CustomTransitionKind":                    "None",
	"CustomTransitionDuration":                "1",
	"AdditionalFontFaceMediaIds":              "[]",
	"CountdownTimeSpan":                       "PT5M",
	"_mainSlideOutput:Width":                  "1920",
	"_mainSlideOutput:Height":                 "1080",
	"confidenceSlideOutput:0:Width":           "1920",
	"confidenceSlideOutput:0:Height":          "1080",
}

// SlideGroup returns the items for a slide deck: a collapsible group named
// title, holding one full-screen image slide per file, and the group's end.
// This is what Proclaim makes when you drop images or a PowerPoint into a
// presentation. Every image is queued for upload.
func SlideGroup(title string, images []string, now time.Time) ([]Item, []Upload, error) {
	if len(images) == 0 {
		return nil, nil, fmt.Errorf("%q has no slides", title)
	}
	start, err := newItem("Grouping", title, now, map[string]string{
		"IsTitleOverridden2":     "true",
		"CollapsibleSectionType": "Start",
		"ShouldLoop":             "False",
	}, "[]")
	if err != nil {
		return nil, nil, err
	}
	items := []Item{start}
	var uploads []Upload
	for _, path := range images {
		it, up, err := localImage(path, now)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, it)
		uploads = append(uploads, up)
	}
	end, err := newItem("Grouping", "Slide Group", now, map[string]string{
		"IsTitleOverridden2":     "false",
		"CollapsibleSectionType": "End",
		"ShouldLoop":             "False",
	}, "[]")
	if err != nil {
		return nil, nil, err
	}
	return append(items, end), uploads, nil
}

// localImage is a slide that shows one image file full screen until
// Proclaim has uploaded it.
func localImage(path string, now time.Time) (Item, Upload, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Item{}, Upload{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Item{}, Upload{}, err
	}
	if info.IsDir() {
		return Item{}, Upload{}, fmt.Errorf("%s is a folder, not an image", path)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	up := Upload{Kind: "Image", JobID: NewID(), FileID: NewID(), Path: path, Name: name, Size: info.Size()}

	jobs, err := json.Marshal([]string{dashed(up.JobID)})
	if err != nil {
		return Item{}, Upload{}, err
	}
	local, err := json.Marshal(map[string]any{
		"uploadJobFileId":                   dashed(up.FileID),
		"name":                              name,
		"path":                              path,
		"loopVideo":                         false,
		"videoDuration":                     "00:00:00",
		"width":                             1920.0,
		"height":                            1080.0,
		"isEmptyForegroundImagePlaceholder": false,
	})
	if err != nil {
		return Item{}, Upload{}, err
	}
	media, err := json.Marshal([]string{localMediaID})
	if err != nil {
		return Item{}, Upload{}, err
	}
	it, err := newItem("Content", name, now, map[string]string{
		"IsTitleOverridden2":                       "false",
		"UploadJobIds":                             string(jobs),
		"localfile:0:" + name:                      string(local),
		"medialayout:0:Settings":                   `{"imageLayout":"uniform","fillColor":"#00FFFFFF","blurImage":false,"rotation":0}`,
		"_mainSlideOutput:MediaLayout:ImageLayout": `"uniform"`,
		"_mainSlideOutput:MediaLayout:FillColor":   `"#00FFFFFF"`,
		"_mainSlideOutput:MediaLayout:BlurImage":   "false",
		"_mainSlideOutput:MediaLayout:Rotation":    "0",
		"_richtextfield:Main Content":              "",
	}, string(media))
	if err != nil {
		return Item{}, Upload{}, err
	}
	up.ServiceItemID = it.ID
	return it, up, nil
}

func newItem(kind, title string, now time.Time, extra map[string]string, media string) (Item, error) {
	c := map[string]string{"RenderVersion": now.UTC().Format("2006-01-02T15:04:05.000Z")}
	for k, v := range itemBase {
		c[k] = v
	}
	for k, v := range extra {
		c[k] = v
	}
	content, err := json.Marshal(c)
	if err != nil {
		return Item{}, err
	}
	return itemFromRow(itemRow{
		ServiceItemID:    NewID(),
		Kind:             kind,
		Title:            title,
		Content:          sql.NullString{String: string(content), Valid: true},
		Notes:            sql.NullString{String: "", Valid: true},
		Media:            sql.NullString{String: media, Valid: true},
		ForegroundImages: sql.NullString{String: "[]", Valid: true},
		Signals:          sql.NullString{String: "[]", Valid: true},
		Modified:         now.UTC().Format(time.RFC3339),
	}), nil
}

// imageProperties is the job file JSON Proclaim writes for an image. Images
// need no transcoding, so Proclaim uploads the file as it is.
func (u Upload) imageProperties(now time.Time) (string, error) {
	b, err := json.Marshal(map[string]any{
		"originalPath": u.Path,
		"mediaInfos": []any{map[string]any{
			"id":       "00000000-0000-0000-0000-000000000000",
			"category": "StillImage",
			"source":   "Upload",
			"about": map[string]any{
				"version": now.UTC().Format(time.RFC3339),
				"name":    u.Name,
				"tags":    []any{},
			},
			"formats": map[string]any{"Custom": map[string]any{
				"textFields": []any{},
				"videoInfo":  map[string]any{"duration": "00:00:00", "loopVideo": false},
			}},
			"isSmartMedia":     false,
			"isNonWatermarked": false,
		}},
		"boardIds":       []any{},
		"contentLength":  0,
		"videoOffset":    0.0,
		"videoDuration":  "00:00:00",
		"needsTranscode": false,
	})
	return string(b), err
}
