# osv2gpx

從 DJI `.OSV` 檔案抽取完整 GPS 軌跡，並輸出成 GPX。

`osv2gpx` 的定位很單純：讀取原始 DJI OSV 容器，從 `djmd` protobuf metadata samples 取出 GPS telemetry，並寫出 GPX 1.1 軌跡檔。工具會保留每個包含 GPS 資料的 metadata sample。

[English](README.md)

## 功能

- 將 DJI `.OSV` GPS telemetry 轉成 GPX。
- 將 OSV 視為 MP4/ISOBMFF 容器讀取。
- 從 `djmd` protobuf metadata 抽取 latitude、longitude 與 absolute altitude。
- 保留每個 GPS metadata sample；不做點位去重。
- 支援選擇性 timestamp offset，方便對齊 DJI SRT 的毫秒時間。

## 需求

- Go 1.26 或更新版本

## 編譯

```powershell
go build -o osv2gpx.exe .
```

## 使用方式

從 `flight.OSV` 產生 `flight.gpx`：

```powershell
.\osv2gpx.exe flight.OSV
```

為每個 OSV 輸入各產生一個 GPX 檔：

```powershell
.\osv2gpx.exe flight1.OSV flight2.OSV flight3.OSV
```

指定輸出路徑：

```powershell
.\osv2gpx.exe -o track.gpx flight.OSV
```

若需要讓 GPX 時間與 DJI SRT 對齊，可套用 timestamp offset：

```powershell
.\osv2gpx.exe -time-offset-ms 647 -o track.gpx flight.OSV
```

若自動偵測 metadata track 不夠，可指定 track：

```powershell
.\osv2gpx.exe -track 3 -o track.gpx flight.OSV
```

從 GPX 第一個時間寫入 MP4 的 QuickTime `creation_time`：

```powershell
.\osv2gpx.exe -mp4time track.gpx video.mp4
```

## 參數

- `-o`：輸出 GPX 路徑。預設為輸入檔名加上 `.gpx` 副檔名。
  只能搭配單一 OSV 輸入使用。
- `-track`：要讀取的 metadata track ID。預設使用 OSV 中第一個 `djmd`
  metadata track。
- `-time-offset-ms`：將所有 GPX 時間加上指定毫秒偏移。
- `-mp4time`：讀取 GPX 第一個 `<time>`，並寫入 MP4 的
  `CreateDate`、`TrackCreateDate` 與 `MediaCreateDate`。

## 輸出

GPX 會包含一個 track segment，內含多個 `trkpt`：

```xml
<trkpt lat="24.79920562" lon="121.05540174">
  <ele>201.138</ele>
  <time>2026-05-27T09:23:16.647Z</time>
</trkpt>
```

高度使用 absolute altitude，單位為 meters。

## Metadata 筆記

目前 DJI Avata 360 範例將 GPS 儲存在 `djmd` protobuf samples 中。payload 中可看到 `dvtm_AVATA360.proto`，但完整 `.proto` schema 沒有嵌入檔案內。GPS 抽取路徑是透過 protobuf wire data 與對應 DJI SRT telemetry 比對後反推。

在 `djmd` 中找到的 GPS 欄位路徑：

```text
telemetry location message: field 4
lat/lon wrapper:            field 4 -> field 1
latitude:                   field 4 -> field 1 -> field 2, fixed64 float64
longitude:                  field 4 -> field 1 -> field 3, fixed64 float64
absolute altitude:          field 4 -> field 2, varint millimeters
relative altitude:          field 5 -> field 1, fixed32 float32 millimeters
```

## 時間筆記

MP4 `creation_time` 只有秒級精度，而 DJI SRT 可能包含毫秒級時間。以測試樣本來說：

```text
SRT 第一筆時間：             2026-05-27T09:23:16.647Z
OSV 第一個 sample 未校正時間：2026-05-27T09:23:16.000Z
```

使用 `-time-offset-ms 647` 可讓 GPX 時間與該 SRT 對齊。

## 轉檔 MP4 限制

轉檔後的 MP4 常只保留 video/audio tracks，不一定會保留 DJI `djmd`、`dbgi` 或 `camd` metadata tracks。如果 metadata tracks 已遺失，就無法抽取 GPS。請盡量使用原始 OSV。

若轉檔後 MP4 遺失 QuickTime creation metadata，但已經有對應 GPX，可使用
`-mp4time` 將 GPX 第一個時間寫入 MP4，讓上傳工具能對齊影片與
GPS 的時間範圍。
