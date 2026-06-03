# osv2gpx

Prepare the GPX track and MP4 timing metadata needed for Google Street View.

`osv2gpx` extracts a matching GPX track from the original DJI `.OSV` file and
can write the GPX timestamp back to the exported MP4 so upload tools can align
the video and GPS track.

[繁體中文](README.zh-TW.md)

## Features

- Converts DJI `.OSV` GPS telemetry to GPX.
- Reads OSV as an MP4/ISOBMFF container.
- Extracts latitude, longitude, and absolute altitude from `djmd` protobuf
  metadata.
- Preserves every GPS metadata sample; no point deduplication is performed.

## Build

```powershell
go build -o osv2gpx.exe .
```

## Usage

First, generate `flight.gpx` from the original `flight.OSV`:

```powershell
osv2gpx flight.OSV
```

Then, write the GPX first timestamp into the DJI Studio exported MP4:

```powershell
osv2gpx flight.mp4 flight.gpx
```

Generate one GPX file per OSV input:

```powershell
osv2gpx flight1.OSV flight2.OSV flight3.OSV
```

Use a specific metadata track if auto-detection is not enough:

```powershell
osv2gpx -track 3 flight.OSV
```

## Options

- `-track`: metadata track ID to read. Defaults to the first `djmd` metadata
  track found in the OSV.
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

Because the schema is missing, the code reads the protobuf wire format directly.
This pseudo `.proto` shows the inferred wire layout. Message and field names are
descriptive placeholders, not official DJI names:

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

Only latitude, longitude, and absolute altitude are written to GPX.

## Timestamp Notes

MP4 creation time fields use the QuickTime epoch (`1904-01-01T00:00:00Z`)
and are second-precision, while DJI SRT files may include millisecond
timestamps. For the tested sample:

```text
SRT first time:        2026-05-27T09:23:16.647Z
OSV first sample time: 2026-05-27T09:23:16.000Z
```

## Exported MP4 Limitation

MP4 files exported from DJI Studio contain only video/audio tracks and do not
preserve DJI `djmd`, `dbgi`, or `camd` metadata tracks. Because those metadata
tracks are missing, GPS cannot be extracted from the exported MP4. Use the
original OSV to generate the GPX track.

DJI Studio exported MP4 files also lack creation time metadata. If you already
have a matching GPX, pass the MP4 and GPX together to copy the GPX's first
timestamp into the MP4 creation time fields so upload tools can align the video
and GPS time ranges.
