package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// schema is the part of PresentationManager.db that proclaimctl touches,
// copied from Proclaim 4.21.
const schema = `
CREATE TABLE Presentations (
	PresentationId text not null primary key, GroupId int, Title text, TransitionKind text not null,
	AspectRatio int, DateGiven text, LogoMediaId text, LogoUploadJobId text, Address text, Location text,
	Latitude double null, Longitude double null, Content text, Modified text, ModifiedUtc text,
	SyncState int not null, SyncRevision integer, IsDeleted bool not null, LastViewed text, LastViewedUtc text,
	PreServiceStartIndex int, StartIndex int, PostServiceStartIndex int, StartTime string, WarmupEnabled bool,
	IsDigitalSignagePresentation bool not null default 0);
CREATE TABLE ServiceItems (
	RecordId integer primary key autoincrement, ServiceItemId text not null, PresentationId text not null,
	ServiceItemKind text not null, Title text not null, Content text, Notes text, Media text,
	ForegroundImages text, Signals text, Modified text not null, CopiedFromServiceItemId text,
	CreatedFromCopyDate text);
CREATE TABLE ServiceItemUploadJobs (ServiceItemId text not null, UploadJobId text not null);`

const uploadSchema = `
CREATE TABLE Info (Version integer not null);
CREATE TABLE UploadJobs (UploadJobId text primary key not null, State int not null, GroupId int not null,
	ServiceItemId text, Name text);
CREATE TABLE UploadFileGroups (UploadFileGroupId text primary key not null, UploadJobId text not null,
	State int not null, Kind int not null);
CREATE TABLE UploadFiles (UploadId text primary key, UploadFileGroupId text not null, Kind text not null,
	State int not null, Properties text not null);
INSERT INTO Info (Version) VALUES (4);`

const bibleContent = `{"References":"[{\"reference\":\"Psalm 23:1–3\",\"searchText\":\"Psalm 23:1–3\",\"syncStatus\":\"noError\"}]",` +
	`"AnimationCache":"{\"referenceCache\":{\"Psalm 23:1–3\":\"<Paragraph />\"},\"additionalContextCache\":{}}","RenderVersion":"2026-01-01T00:00:00.000Z"}`

const announcementContent = `{"_richtextfield:Main":"<Paragraph><Run Text=\"Bible study 4 October\" /></Paragraph>"}`

// fixture builds a database with two past services and an upcoming one.
func fixture(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	for _, dir := range []string{"PresentationManager", "UploadManager"} {
		if err := os.Mkdir(filepath.Join(data, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	uploads, err := sql.Open("sqlite", filepath.Join(data, "UploadManager", "UploadManager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer uploads.Close()
	if _, err := uploads.Exec(uploadSchema); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(data, "PresentationManager", "PresentationManager.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(schema)
	pres := func(id, title, date string, pre, start, post int) {
		exec(`INSERT INTO Presentations (PresentationId, GroupId, Title, TransitionKind, DateGiven, Content,
			ModifiedUtc, SyncState, SyncRevision, IsDeleted, PreServiceStartIndex, StartIndex, PostServiceStartIndex, StartTime)
			VALUES (?, 42, ?, 'Fade', ?, '{"PresentationStartTimes":"[\"10:30:00\"]"}', '2026-01-01T00:00:00Z', 3, 100, 0, ?, ?, ?, '10:30:00')`,
			id, title, date, pre, start, post)
	}
	item := func(pres, id, kind, title, content string) {
		exec(`INSERT INTO ServiceItems (ServiceItemId, PresentationId, ServiceItemKind, Title, Content, Modified)
			VALUES (?, ?, ?, ?, ?, '2026-01-01T00:00:00Z')`, id, pres, kind, title, content)
	}

	pres("old", "Service 1 March", "2026-03-01", 0, 0, 2)
	item("old", "old-song", "SongLyrics", "Amazing Grace", `{"_richtextfield:Lyrics":"old words"}`)
	item("old", "old-bible", "OnScreenBible", "On-Screen Bible", bibleContent)
	item("old", "old-video", "Video", "Mission film", `{"UploadJobIds":"[]","localfile:0:old":"{}","medialayout:0:Settings":"{}"}`)

	pres("newer", "Service 5 April", "2026-04-05", 0, 0, 1)
	item("newer", "newer-song", "SongLyrics", "Amazing Grace", `{"_richtextfield:Lyrics":"new words"}`)

	pres("next", "Service 12 April", "2026-04-12", 1, 1, 4)
	item("next", "loop", "Content", "Welcome", `{}`)
	item("next", "creed", "SongLyrics", "Creed", `{}`)
	item("next", "kids", "Content", "Sunday school", `{}`)
	item("next", "notice", "Content", "Bible study", announcementContent)
	item("next", "outro", "Content", "Welcome", `{}`)
	return path
}

type run struct {
	code   int
	stdout string
	stderr string
}

func runApp(t *testing.T, dbPath string, running bool, args ...string) run {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := New(&stdout, &stderr, Build{Version: "test", Commit: "unknown", Date: "unknown"})
	app.LocateDB = func() (string, error) { return dbPath, nil }
	app.ProclaimRunning = func() bool { return running }
	app.BackupDir = filepath.Join(filepath.Dir(dbPath), "backups")
	app.Now = func() time.Time { return time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC) }
	code := app.Run(args)
	return run{code, stdout.String(), stderr.String()}
}

func writePlan(t *testing.T, plan string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type row struct {
	id, kind, title, content string
}

func items(t *testing.T, dbPath, presentation string) []row {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT ServiceItemId, ServiceItemKind, Title, coalesce(Content, '') FROM ServiceItems
		WHERE PresentationId = ? ORDER BY RecordId`, presentation)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.kind, &r.title, &r.content); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func presentationRow(t *testing.T, dbPath, where string, args ...any) map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, title, date, start, content string
	var state, pre, svc, post int
	var rev sql.NullInt64
	err = db.QueryRow(`SELECT PresentationId, Title, DateGiven, StartTime, Content, SyncState, SyncRevision,
		PreServiceStartIndex, StartIndex, PostServiceStartIndex FROM Presentations WHERE `+where, args...).
		Scan(&id, &title, &date, &start, &content, &state, &rev, &pre, &svc, &post)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": id, "title": title, "date": date, "start": start, "content": content,
		"state": state, "revision": rev, "bounds": [3]int{pre, svc, post}}
}

const nextPlan = `{
	"presentation": "Service 12 April",
	"start_time": "11:00",
	"service": [
		{"reuse": "amazing grace", "kind": "SongLyrics"},
		{"bible": "John 11:1–7"},
		{"keep": "Creed"},
		{"keep": "Bible study", "replace": {"4 October": "18 October"}}
	]
}`

func TestApplyPreviewWritesNothing(t *testing.T) {
	db := fixture(t)
	r := runApp(t, db, true, "apply", writePlan(t, nextPlan))
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "Preview only") || !strings.Contains(r.stdout, "Amazing Grace [SongLyrics]") {
		t.Errorf("preview output:\n%s", r.stdout)
	}
	if got := presentationRow(t, db, "PresentationId = 'next'")["state"]; got != 3 {
		t.Errorf("SyncState = %v after a preview", got)
	}
}

func TestApplyRefusesWhileProclaimRuns(t *testing.T) {
	db := fixture(t)
	r := runApp(t, db, true, "apply", writePlan(t, nextPlan), "--yes")
	if r.code != exitError || !strings.Contains(r.stderr, "Proclaim is running") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestApplyRewritesPresentation(t *testing.T) {
	db := fixture(t)
	r := runApp(t, db, false, "apply", writePlan(t, nextPlan), "--yes")
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}

	got := items(t, db, "next")
	var titles []string
	for _, it := range got {
		titles = append(titles, it.title)
	}
	want := "Welcome|Amazing Grace|On-Screen Bible|Creed|Bible study|Welcome"
	if strings.Join(titles, "|") != want {
		t.Fatalf("order = %s, want %s", strings.Join(titles, "|"), want)
	}

	if got[1].content != `{"_richtextfield:Lyrics":"new words"}` || got[1].id == "newer-song" {
		t.Errorf("song should be a copy of the newest use under a new id, got %+v", got[1])
	}
	if got[0].id != "loop" || got[3].id != "creed" || got[5].id != "outro" {
		t.Errorf("kept items must keep their ids: %+v", got)
	}

	var bible map[string]string
	if err := json.Unmarshal([]byte(got[2].content), &bible); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bible["References"], "John 11:1–7") || strings.Contains(bible["References"], "Psalm") {
		t.Errorf("References = %s", bible["References"])
	}
	if strings.Contains(bible["AnimationCache"], "Psalm") || !strings.Contains(bible["AnimationCache"], "additionalContextCache") {
		t.Errorf("AnimationCache = %s, want the passage cache cleared and the rest kept", bible["AnimationCache"])
	}
	if !strings.Contains(got[4].content, "Bible study 18 October") {
		t.Errorf("replace did not change the text: %s", got[4].content)
	}

	p := presentationRow(t, db, "PresentationId = 'next'")
	if p["state"] != 1 {
		t.Errorf("SyncState = %v, want 1 so Proclaim uploads it", p["state"])
	}
	if p["bounds"] != [3]int{1, 1, 5} {
		t.Errorf("section bounds = %v", p["bounds"])
	}
	if p["start"] != "11:00:00" || !strings.Contains(p["content"].(string), `11:00:00`) {
		t.Errorf("start time = %v, content %v", p["start"], p["content"])
	}

	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(db), "backups", "*.db"))
	if len(backups) != 1 {
		t.Errorf("backups = %v", backups)
	}
}

func TestKeepSkipsItemsOfSectionsLeftOut(t *testing.T) {
	db := fixture(t)
	plan := `{"presentation": "next", "service": [{"keep": "Welcome"}]}`
	r := runApp(t, db, false, "apply", writePlan(t, plan), "--yes")
	if r.code != exitError || !strings.Contains(r.stderr, `no unused item "Welcome"`) {
		t.Fatalf("keep must not take the post-service Welcome: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestApplyQueuesVideoUpload(t *testing.T) {
	db := fixture(t)
	film := filepath.Join(t.TempDir(), "Mission week.mp4")
	if err := os.WriteFile(film, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := `{"presentation": "next", "service": [{"keep": "Creed"}, {"video": ` + strconvQuote(film) + `}]}`
	r := runApp(t, db, false, "apply", writePlan(t, plan), "--yes")
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}

	got := items(t, db, "next")
	video := got[2]
	if video.kind != "Video" || video.title != "Mission week" || video.id == "old-video" {
		t.Fatalf("video item = %+v", video)
	}
	var content map[string]string
	if err := json.Unmarshal([]byte(video.content), &content); err != nil {
		t.Fatal(err)
	}
	if _, ok := content["localfile:0:old"]; ok {
		t.Error("the template's local file must be removed")
	}
	if !strings.Contains(content["localfile:0:Mission week"], film) {
		t.Errorf("local file = %q", content["localfile:0:Mission week"])
	}

	up, err := sql.Open("sqlite", filepath.Join(filepath.Dir(filepath.Dir(db)), "UploadManager", "UploadManager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	var job, group, serviceItem, props string
	err = up.QueryRow(`SELECT j.UploadJobId, j.GroupId, j.ServiceItemId, f.Properties FROM UploadJobs j
		JOIN UploadFileGroups g USING (UploadJobId) JOIN UploadFiles f USING (UploadFileGroupId)
		WHERE j.State = 0 AND g.Kind = 0 AND f.Kind = 'Video'`).Scan(&job, &group, &serviceItem, &props)
	if err != nil {
		t.Fatalf("queued upload: %v", err)
	}
	if group != "42" || serviceItem != video.id {
		t.Errorf("job group %s, service item %s, want 42 and %s", group, serviceItem, video.id)
	}
	if !strings.Contains(content["UploadJobIds"], job[:8]+"-") {
		t.Errorf("UploadJobIds = %s, want job %s", content["UploadJobIds"], job)
	}
	for _, want := range []string{`"needsTranscode":true`, `"videoInfo":{}`, `"Custom"`, `"name":"Mission week"`} {
		if !strings.Contains(props, want) {
			t.Errorf("properties lack %s: %s", want, props)
		}
	}

	r = runApp(t, db, false, "show", "next")
	if !strings.Contains(r.stdout, `Media on "Mission week" is not uploaded yet.`) || !strings.Contains(r.stdout, "Changed locally") {
		t.Errorf("show must say the presentation is not done:\n%s", r.stdout)
	}

	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(db), "backups", "UploadManager-*.db"))
	if len(backups) != 1 {
		t.Errorf("upload queue backups = %v", backups)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestApplyFromCreatesNewPresentation(t *testing.T) {
	db := fixture(t)
	plan := `{"from": "Service 12 April", "title": "Service 19 April", "date": "2026-04-19",
		"service": [{"keep": "Creed"}]}`
	r := runApp(t, db, false, "apply", writePlan(t, plan), "--yes")
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	p := presentationRow(t, db, "Title = 'Service 19 April'")
	if p["state"] != 1 || p["revision"].(sql.NullInt64).Valid || p["date"] != "2026-04-19" {
		t.Errorf("new presentation = %v", p)
	}
	got := items(t, db, p["id"].(string))
	if len(got) != 3 || got[0].title != "Welcome" || got[1].title != "Creed" || got[2].title != "Welcome" {
		t.Fatalf("items = %+v", got)
	}
	for _, it := range got {
		if it.id == "loop" || it.id == "creed" || it.id == "outro" {
			t.Errorf("a copied presentation must not share item ids with its source: %s", it.id)
		}
	}
	if len(items(t, db, "next")) != 5 {
		t.Error("the source presentation changed")
	}
}

func TestApplyReportsUnknownItem(t *testing.T) {
	db := fixture(t)
	plan := `{"presentation": "next", "service": [{"reuse": "No Such Song", "kind": "SongLyrics"}]}`
	r := runApp(t, db, false, "apply", writePlan(t, plan), "--yes")
	if r.code != exitError || !strings.Contains(r.stderr, `no SongLyrics item titled "No Such Song"`) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if presentationRow(t, db, "PresentationId = 'next'")["state"] != 3 {
		t.Error("a failed plan must not write")
	}
}

func TestPlanRejectsTypos(t *testing.T) {
	db := fixture(t)
	r := runApp(t, db, false, "apply", writePlan(t, `{"presentation": "next", "servise": []}`))
	if r.code != exitError || !strings.Contains(r.stderr, "servise") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestShowAndFind(t *testing.T) {
	db := fixture(t)
	r := runApp(t, db, false, "show", "12 april")
	if r.code != exitOK || !strings.Contains(r.stdout, "## Service (3)") || !strings.Contains(r.stdout, "id: creed") {
		t.Fatalf("show: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}

	r = runApp(t, db, false, "show", "Service")
	if r.code != exitError || !strings.Contains(r.stderr, "matches 3 presentations") {
		t.Fatalf("ambiguous show: exit %d, stderr %q", r.code, r.stderr)
	}

	r = runApp(t, db, false, "find", "grace", "--json")
	var uses []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &uses); err != nil {
		t.Fatalf("find --json: %v\n%s", err, r.stdout)
	}
	if len(uses) != 1 || uses[0]["uses"] != float64(2) || uses[0]["last_date"] != "2026-04-05" {
		t.Errorf("find = %v", uses)
	}
}
