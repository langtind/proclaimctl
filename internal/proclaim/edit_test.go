package proclaim

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestWithBibleReferenceSplitsPassages(t *testing.T) {
	it := itemFromRow(itemRow{
		ServiceItemID: "bible",
		Kind:          "OnScreenBible",
		Title:         "On-Screen Bible",
		Content:       sql.NullString{String: `{"References":"[]"}`, Valid: true},
	})

	got, err := it.WithBibleReference("Isaiah 57:14–16; Revelation 20:11–13", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Isaiah 57:14–16", "Revelation 20:11–13"}
	if !reflect.DeepEqual(got.References, want) {
		t.Errorf("References = %q, want %q", got.References, want)
	}
	if again := itemFromRow(got.row); !reflect.DeepEqual(again.References, want) {
		t.Errorf("stored references read back as %q, want %q", again.References, want)
	}

	var content map[string]string
	if err := json.Unmarshal([]byte(got.row.Content.String), &content); err != nil {
		t.Fatal(err)
	}
	var refs []map[string]string
	if err := json.Unmarshal([]byte(content["References"]), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0]["reference"] != want[0] || refs[1]["reference"] != want[1] || refs[2]["reference"] != "" {
		t.Errorf("References JSON = %v, want one entry per passage and an empty one last", refs)
	}
}
