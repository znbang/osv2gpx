# osv2gpx

準備 Google Street View 所需的 GPX 軌跡與 MP4 時間資訊。

`osv2gpx` 會從原始 DJI `.OSV` 檔案抽出對應的 GPX 軌跡，也可以將 GPX 的時間
寫回匯出的 MP4，讓上傳工具能對齊影片與 GPS 軌跡。

[English](README.md)

## 功能

- 將 DJI `.OSV` GPS telemetry 轉成 GPX。
- 將 OSV 視為 MP4/ISOBMFF 容器讀取。
- 從 `djmd` protobuf metadata 抽取 latitude、longitude 與 absolute altitude。
- 保留每個 GPS metadata sample；不做點位去重。

## 編譯

```powershell
go build -o osv2gpx.exe .
```

## 使用方式

先從原始 `flight.OSV` 產生 `flight.gpx`：

```powershell
osv2gpx flight.OSV
```

再將 GPX 第一個時間寫入 DJI Studio 匯出的 MP4：

```powershell
osv2gpx flight.mp4 flight.gpx
```

為每個 OSV 輸入各產生一個 GPX 檔：

```powershell
osv2gpx flight1.OSV flight2.OSV flight3.OSV
```

若自動偵測 metadata track 不夠，可指定 track：

```powershell
osv2gpx -track 3 flight.OSV
```

## 參數

- `-track`：要讀取的 metadata track ID。預設使用 OSV 中第一個 `djmd`
  metadata track。
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

因為檔案裡沒有完整 schema，程式會直接讀 protobuf wire format。下面是用
pseudo `.proto` 表示的反推 wire layout。message 與 field 名稱只是描述用，
不是 DJI 官方名稱：

```proto
message Telemetry {
  Location location = 4;
  RelativeAltitudeWrapper relative_altitude = 5;
}

message Location {
  Coordinates coordinates = 1;
  uint64 absolute_altitude_mm = 2;
}

message Coordinates {
  fixed64 reserved_or_unknown = 1;
  fixed64 latitude = 2;   // decoded as float64
  fixed64 longitude = 3;  // decoded as float64
}

message RelativeAltitudeWrapper {
  fixed32 relative_altitude_mm = 1;  // decoded as float32
}
```

GPX 只會寫出 latitude、longitude 與 absolute altitude。

## 時間筆記

MP4 creation time 欄位使用 QuickTime epoch（`1904-01-01T00:00:00Z`），
且只有秒級精度，而 DJI SRT 可能包含毫秒級時間。以測試樣本來說：

```text
SRT 第一筆時間：       2026-05-27T09:23:16.647Z
OSV 第一個 sample 時間：2026-05-27T09:23:16.000Z
```

## DJI Studio 匯出 MP4 限制

DJI Studio 匯出的 MP4 只包含 video/audio tracks，不會保留 DJI `djmd`、
`dbgi` 或 `camd` metadata tracks。因為這些 metadata tracks 不存在，
所以無法從匯出的 MP4 抽取 GPS。請使用原始 OSV 產生 GPX 軌跡。

DJI Studio 匯出的 MP4 也沒有 creation time metadata。若已經有對應 GPX，
可同時傳入 MP4 與 GPX，將 GPX 第一個時間寫入 MP4 creation time 欄位，讓
上傳工具能對齊影片與 GPS 的時間範圍。
