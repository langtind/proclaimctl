# Proclaim database notes

Observed on Proclaim 4.20 and 4.21 for macOS. Paths are relative to `~/Library/Application Support/Proclaim/Data/<account>/` on macOS and `%LOCALAPPDATA%\Proclaim\Data\<account>\` on Windows.

## PresentationManager/PresentationManager.db

### Presentations

One row per presentation. Ids are GUIDs as 32 lowercase hex digits.

| Column | Meaning |
| --- | --- |
| `SyncState` | `1` changed locally, `2` uploading, `3` in sync. Proclaim uploads every row with `1` within a minute of starting, and then sets `3` and a new `SyncRevision`. |
| `SyncRevision` | The server revision. `NULL` on a presentation that has never been uploaded. |
| `PreServiceStartIndex`, `StartIndex`, `PostServiceStartIndex` | Item indexes where the warm-up, service and post-service sections start. Items before the first index are the pre-service loop. |
| `StartTime` | Service start, `HH:MM:SS`. The `Content` JSON repeats it in `PresentationStartTimes`. |
| `IsDeleted` | Deleted presentations stay as rows with `1`. |

### ServiceItems

One row per item. Proclaim deletes and inserts all of a presentation's rows on every save, so `RecordId` order is presentation order. Moving an item in the app gives every item a new `RecordId` and keeps its `ServiceItemId`.

`Content` is a JSON object whose values are strings. Several values are JSON encoded a second time.

| Kind | Notes |
| --- | --- |
| `SongLyrics` | `_richtextfield:Lyrics` holds the lyrics as rich text XML. `ProclaimArrangement` links the song to Faithlife Music. Copying the row under a new `ServiceItemId` reuses the song. |
| `OnScreenBible` | `References` lists the passages, such as `John 11:1–7` with an en dash. `AnimationCache.referenceCache` holds the passage text. With the cache emptied, Proclaim fetches the text from the presentation's licensed Bible (`CurrentBibleId`) the next time it renders the item. |
| `Clone` | Shows another item. `CloneToken` links the clone to its original, and duplicated presentations keep the token. |
| `Content` | Slides with free text. Text sits in `Run Text="..."` XML inside the JSON. |

Setting `RenderVersion` in `Content` to a new timestamp makes Proclaim render the item's slides again.

## Video uploads

A `Video` item can point at a local file before it is uploaded. Proclaim then uploads the file on its own.

- The item's `Media` column holds the fixed local media id `["fcfb311c-1b42-4eae-b4b6-7f0d2b4b4860"]`.
- `Content` has `UploadJobIds`, a JSON list with the job id as a dashed GUID, and a key `localfile:0:<name>` holding `{"uploadJobFileId", "name", "path", "videoDuration", "loopVideo"}`.
- `ServiceItemUploadJobs` links the item to the job.

`UploadManager/UploadManager.db` holds the queue. Proclaim loads pending jobs (`State` 0) when it starts and runs the upload step only. A job needs one row in each table:

| Table | Values |
| --- | --- |
| `UploadJobs` | `State` 0, `GroupId` from the presentation, `ServiceItemId` of the item |
| `UploadFileGroups` | `State` 0, `Kind` 0 (group upload) |
| `UploadFiles` | `Kind` `Video`, `State` 0, `Properties` as below |

`Properties` is camelCase JSON. With `needsTranscode` true, Faithlife's server makes the preview images, so no local images are needed. `videoInfo` must be present, and `mediaInfos` must hold one entry with `formats.Custom`.

```json
{"originalPath": "/path/film.mp4", "needsTranscode": true, "videoOffset": 11.0,
 "videoDuration": "00:00:00", "contentLength": 87031234, "videoInfo": {},
 "mediaInfos": [{"source": "Upload", "category": "Motion",
   "about": {"version": "2026-10-10T21:30:00Z", "name": "film"},
   "formats": {"Custom": {"textFields": [], "videoInfo": {}}}}]}
```

When the upload completes, Proclaim replaces the local media id with the uploaded media id, removes the job from `UploadJobIds`, and sets `SyncState` 1. Observed on Proclaim 4.21: an 83 MB file uploaded, transcoded and swapped in within 30 seconds of start.
