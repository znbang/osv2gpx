# osv2gpx

Extract a complete GPS track from a DJI `.OSV` file and write it as GPX.

`osv2gpx` is intentionally small: it takes an original DJI OSV container, reads
the GPS telemetry stored in `djmd` protobuf metadata samples, and writes a GPX
1.1 track file. It preserves every metadata sample that contains GPS data.

[繁體中文](README.zh-TW.md)

## Features

- Converts DJI `.OSV` GPS telemetry to GPX.
- Reads OSV as an MP4/ISOBMFF container.
- Extracts latitude, longitude, and absolute altitude from `djmd` protobuf
  metadata.
- Preserves every GPS metadata sample; no point deduplication is performed.
- Supports an optional timestamp offset for matching DJI SRT millisecond timing.

## Requirements

- Go 1.26 or newer

## Build

```powershell
go build -o osv2gpx.exe .
```

## Usage

Generate `flight.gpx` from `flight.OSV`:

```powershell
.\osv2gpx.exe flight.OSV
```

Generate one GPX file per OSV input:

```powershell
.\osv2gpx.exe flight1.OSV flight2.OSV flight3.OSV
```

Specify the output path:

```powershell
.\osv2gpx.exe -o track.gpx flight.OSV
```

Apply a timestamp offset when you need to align GPX timestamps with a DJI SRT:

```powershell
.\osv2gpx.exe -time-offset-ms 647 -o track.gpx flight.OSV
```

Use a specific metadata track if auto-detection is not enough:

```powershell
.\osv2gpx.exe -track 3 -o track.gpx flight.OSV
```

Set an MP4 QuickTime `creation_time` from the first timestamp in a GPX:

```powershell
.\osv2gpx.exe -mp4time track.gpx video.mp4
```

## Options

- `-o`: output GPX path. Defaults to the input filename with `.gpx` extension.
  Can only be used with one OSV input.
- `-track`: metadata track ID to read. Defaults to the first `djmd` metadata
  track found in the OSV.
- `-time-offset-ms`: milliseconds to add to all GPX timestamps.
- `-mp4time`: read the first `<time>` from a GPX and write it to
  the MP4 `CreateDate`, `TrackCreateDate`, and `MediaCreateDate` fields.

## Output

The GPX output contains one track segment with `trkpt` entries:

```xml
<trkpt lat="24.79920562" lon="121.05540174">
  <ele>201.138</ele>
  <time>2026-05-27T09:23:16.647Z</time>
</trkpt>
```

Elevation uses absolute altitude in meters.

## Metadata Notes

The current DJI Avata 360 sample stores GPS in `djmd` protobuf samples. The
payload advertises `dvtm_AVATA360.proto`, but the full `.proto` schema is not
embedded in the file. The GPS extraction path was inferred from protobuf wire
data and checked against the matching DJI SRT telemetry.

GPS field path found in `djmd`:

```text
telemetry location message: field 4
lat/lon wrapper:            field 4 -> field 1
latitude:                   field 4 -> field 1 -> field 2, fixed64 float64
longitude:                  field 4 -> field 1 -> field 3, fixed64 float64
absolute altitude:          field 4 -> field 2, varint millimeters
relative altitude:          field 5 -> field 1, fixed32 float32 millimeters
```

## Timestamp Notes

MP4 `creation_time` is second-precision, while DJI SRT files may include
millisecond timestamps. For the tested sample:

```text
SRT first time:              2026-05-27T09:23:16.647Z
OSV first sample before fix: 2026-05-27T09:23:16.000Z
```

Use `-time-offset-ms 647` to align GPX timestamps with that SRT.

## Converted MP4 Limitation

Converted MP4 files often contain only video/audio tracks and may not preserve
DJI `djmd`, `dbgi`, or `camd` metadata tracks. If those metadata tracks are
missing, GPS cannot be extracted. Use the original OSV whenever possible.

If a converted MP4 lost its QuickTime creation metadata but you already have a
matching GPX, use `-mp4time` to copy the GPX's first timestamp into
the MP4 so upload tools can align the video and GPS time ranges.
