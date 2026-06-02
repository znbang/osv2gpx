package main

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type boxHeader struct {
	typ        string
	headerSize int64
	size       int64
	start      int64
	end        int64
}

type track struct {
	ID          uint32 `json:"id"`
	Handler     string `json:"handler"`
	Name        string `json:"name,omitempty"`
	SampleEntry string `json:"sample_entry,omitempty"`
	TimeScale   uint32 `json:"timescale,omitempty"`
	SampleCount int    `json:"sample_count"`

	sizes        []uint32
	chunkOffsets []uint64
	stsc         []stscEntry
	stts         []sttsEntry
}

type stscEntry struct {
	FirstChunk             uint32
	SamplesPerChunk        uint32
	SampleDescriptionIndex uint32
}

type sttsEntry struct {
	SampleCount uint32
	SampleDelta uint32
}

type sampleRef struct {
	Index  int
	Offset int64
	Size   uint32
	Time   float64
}

type gpsPoint struct {
	Lat    float64
	Lon    float64
	AbsAlt float64
	RelAlt float64
	Time   time.Time
}

type protoField struct {
	Number  uint64
	Wire    uint64
	Bytes   []byte
	Uint    uint64
	Fixed32 uint32
	Fixed64 uint64
}

func main() {
	trackID := flag.Uint("track", 0, "metadata track id to use; defaults to first djmd track")
	output := flag.String("o", "", "output GPX path; default is input filename with .gpx extension")
	timeOffsetMS := flag.Int("time-offset-ms", 0, "milliseconds to add to MP4 sample time when writing GPX")
	mp4Time := flag.Bool("mp4time", false, "set an MP4 creation_time from the first timestamp in a GPX; args: file.gpx file.mp4")
	flag.Parse()

	if *mp4Time {
		if flag.NArg() != 2 {
			fmt.Fprintln(os.Stderr, "usage: osv2gpx -mp4time [flags] file.gpx file.mp4")
			flag.PrintDefaults()
			os.Exit(2)
		}
		if err := setMP4CreationTimeFromGPX(flag.Arg(0), flag.Arg(1)); err != nil {
			fatal(err)
		}
		return
	}

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: osv2gpx [flags] file.OSV [file2.OSV ...]")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if *output != "" && flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "error: -o can only be used with one OSV input")
		os.Exit(2)
	}

	for _, path := range flag.Args() {
		if err := convertOSVToGPX(path, *output, uint32(*trackID), *timeOffsetMS); err != nil {
			fatal(fmt.Errorf("%s: %w", path, err))
		}
	}
}

func convertOSVToGPX(path, outputPath string, trackID uint32, timeOffsetMS int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tracks, err := parseTracks(f)
	if err != nil {
		return err
	}
	creationTime, err := parseMovieCreationTime(f)
	if err != nil {
		return err
	}

	t, err := selectTrack(tracks, trackID)
	if err != nil {
		return err
	}
	points, err := extractGPSPoints(f, t, creationTime.Add(time.Duration(timeOffsetMS)*time.Millisecond))
	if err != nil {
		return err
	}
	if len(points) == 0 {
		return errors.New("no GPS points found in OSV protobuf metadata")
	}

	if outputPath == "" {
		outputPath = strings.TrimSuffix(path, filepath.Ext(path)) + ".gpx"
	}
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()
	if err := writeGPX(outFile, points, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d GPX points to %s\n", len(points), outputPath)
	return nil
}

func parseTracks(r io.ReaderAt) ([]*track, error) {
	size, err := readerSize(r)
	if err != nil {
		return nil, err
	}
	var tracks []*track
	for _, top := range readBoxes(r, 0, size) {
		if top.typ != "moov" {
			continue
		}
		for _, child := range readBoxes(r, top.start+top.headerSize, top.end) {
			if child.typ != "trak" {
				continue
			}
			t, err := parseTrack(r, child)
			if err != nil {
				return nil, err
			}
			tracks = append(tracks, t)
		}
	}
	return tracks, nil
}

func parseMovieCreationTime(r io.ReaderAt) (time.Time, error) {
	size, err := readerSize(r)
	if err != nil {
		return time.Time{}, err
	}
	for _, top := range readBoxes(r, 0, size) {
		if top.typ != "moov" {
			continue
		}
		for _, child := range readBoxes(r, top.start+top.headerSize, top.end) {
			if child.typ != "mvhd" {
				continue
			}
			b, err := readAt(r, child.start+child.headerSize, 20)
			if err != nil {
				return time.Time{}, err
			}
			var seconds uint64
			if b[0] == 1 {
				b, err = readAt(r, child.start+child.headerSize, 28)
				if err != nil {
					return time.Time{}, err
				}
				seconds = binary.BigEndian.Uint64(b[4:12])
			} else {
				seconds = uint64(binary.BigEndian.Uint32(b[4:8]))
			}
			quickTimeEpoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
			return quickTimeEpoch.Add(time.Duration(seconds) * time.Second), nil
		}
	}
	return time.Time{}, errors.New("mvhd creation time not found")
}

func parseTrack(r io.ReaderAt, trak boxHeader) (*track, error) {
	t := &track{}
	for _, box := range readBoxes(r, trak.start+trak.headerSize, trak.end) {
		switch box.typ {
		case "tkhd":
			id, err := parseTKHD(r, box)
			if err != nil {
				return nil, err
			}
			t.ID = id
		case "mdia":
			if err := parseMDIA(r, box, t); err != nil {
				return nil, err
			}
		}
	}
	t.SampleCount = len(t.sizes)
	return t, nil
}

func parseMDIA(r io.ReaderAt, mdia boxHeader, t *track) error {
	for _, box := range readBoxes(r, mdia.start+mdia.headerSize, mdia.end) {
		switch box.typ {
		case "mdhd":
			ts, err := parseMDHD(r, box)
			if err != nil {
				return err
			}
			t.TimeScale = ts
		case "hdlr":
			handler, name, err := parseHDLR(r, box)
			if err != nil {
				return err
			}
			t.Handler = handler
			t.Name = name
		case "minf":
			if err := parseMINF(r, box, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseMINF(r io.ReaderAt, minf boxHeader, t *track) error {
	for _, box := range readBoxes(r, minf.start+minf.headerSize, minf.end) {
		if box.typ != "stbl" {
			continue
		}
		for _, stbl := range readBoxes(r, box.start+box.headerSize, box.end) {
			var err error
			switch stbl.typ {
			case "stsd":
				t.SampleEntry, err = parseSTSD(r, stbl)
			case "stsz":
				t.sizes, err = parseSTSZ(r, stbl)
			case "stco":
				t.chunkOffsets, err = parseSTCO(r, stbl)
			case "co64":
				t.chunkOffsets, err = parseCO64(r, stbl)
			case "stsc":
				t.stsc, err = parseSTSC(r, stbl)
			case "stts":
				t.stts, err = parseSTTS(r, stbl)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *track) samples() ([]sampleRef, error) {
	if len(t.sizes) == 0 {
		return nil, errors.New("track has no sample sizes")
	}
	if len(t.chunkOffsets) == 0 {
		return nil, errors.New("track has no chunk offsets")
	}
	if len(t.stsc) == 0 {
		return nil, errors.New("track has no sample-to-chunk table")
	}
	sort.Slice(t.stsc, func(i, j int) bool {
		return t.stsc[i].FirstChunk < t.stsc[j].FirstChunk
	})

	refs := make([]sampleRef, 0, len(t.sizes))
	sampleIndex := 0
	times := t.sampleTimes()
	for chunkIdx, chunkOffset := range t.chunkOffsets {
		entry := t.stsc[0]
		chunkNo := uint32(chunkIdx + 1)
		for i := range t.stsc {
			if t.stsc[i].FirstChunk <= chunkNo {
				entry = t.stsc[i]
			}
		}

		offset := int64(chunkOffset)
		for j := uint32(0); j < entry.SamplesPerChunk && sampleIndex < len(t.sizes); j++ {
			size := t.sizes[sampleIndex]
			sampleTime := 0.0
			if sampleIndex < len(times) {
				sampleTime = times[sampleIndex]
			}
			refs = append(refs, sampleRef{Index: sampleIndex + 1, Offset: offset, Size: size, Time: sampleTime})
			offset += int64(size)
			sampleIndex++
		}
	}
	if len(refs) != len(t.sizes) {
		return nil, fmt.Errorf("built %d sample refs for %d sample sizes", len(refs), len(t.sizes))
	}
	return refs, nil
}

func (t *track) sampleTimes() []float64 {
	times := make([]float64, len(t.sizes))
	if t.TimeScale == 0 {
		return times
	}
	var ticks uint64
	idx := 0
	for _, entry := range t.stts {
		for i := uint32(0); i < entry.SampleCount && idx < len(times); i++ {
			times[idx] = float64(ticks) / float64(t.TimeScale)
			ticks += uint64(entry.SampleDelta)
			idx++
		}
	}
	return times
}

func selectTrack(tracks []*track, id uint32) (*track, error) {
	if id != 0 {
		for _, t := range tracks {
			if t.ID == id {
				return t, nil
			}
		}
		return nil, fmt.Errorf("track id %d not found", id)
	}
	for _, t := range tracks {
		if t.SampleEntry == "djmd" || t.Handler == "meta" {
			return t, nil
		}
	}
	return nil, errors.New("no djmd/meta track found; pass -track explicitly")
}

func readBoxes(r io.ReaderAt, start, end int64) []boxHeader {
	var boxes []boxHeader
	for off := start; off+8 <= end; {
		buf, err := readAt(r, off, 16)
		if err != nil {
			break
		}
		size32 := binary.BigEndian.Uint32(buf[0:4])
		typ := string(buf[4:8])
		headerSize := int64(8)
		size := int64(size32)
		if size32 == 1 {
			size = int64(binary.BigEndian.Uint64(buf[8:16]))
			headerSize = 16
		} else if size32 == 0 {
			size = end - off
		}
		if size < headerSize || off+size > end {
			break
		}
		boxes = append(boxes, boxHeader{typ: typ, headerSize: headerSize, size: size, start: off, end: off + size})
		off += size
	}
	return boxes
}

func parseTKHD(r io.ReaderAt, box boxHeader) (uint32, error) {
	b, err := readAt(r, box.start+box.headerSize, 24)
	if err != nil {
		return 0, err
	}
	version := b[0]
	if version == 1 {
		b, err = readAt(r, box.start+box.headerSize, 36)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint32(b[20:24]), nil
	}
	return binary.BigEndian.Uint32(b[12:16]), nil
}

func parseMDHD(r io.ReaderAt, box boxHeader) (uint32, error) {
	b, err := readAt(r, box.start+box.headerSize, 24)
	if err != nil {
		return 0, err
	}
	if b[0] == 1 {
		b, err = readAt(r, box.start+box.headerSize, 32)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint32(b[20:24]), nil
	}
	return binary.BigEndian.Uint32(b[12:16]), nil
}

func parseHDLR(r io.ReaderAt, box boxHeader) (string, string, error) {
	size := int(box.size - box.headerSize)
	b, err := readAt(r, box.start+box.headerSize, size)
	if err != nil {
		return "", "", err
	}
	if len(b) < 24 {
		return "", "", errors.New("short hdlr")
	}
	handler := string(b[8:12])
	name := strings.TrimRight(string(b[24:]), "\x00")
	return handler, name, nil
}

func parseSTSD(r io.ReaderAt, box boxHeader) (string, error) {
	b, err := readAt(r, box.start+box.headerSize, 16)
	if err != nil {
		return "", err
	}
	if binary.BigEndian.Uint32(b[4:8]) == 0 {
		return "", nil
	}
	return string(b[12:16]), nil
}

func parseSTSZ(r io.ReaderAt, box boxHeader) ([]uint32, error) {
	b, err := readAt(r, box.start+box.headerSize, 12)
	if err != nil {
		return nil, err
	}
	sampleSize := binary.BigEndian.Uint32(b[4:8])
	count := binary.BigEndian.Uint32(b[8:12])
	sizes := make([]uint32, int(count))
	if sampleSize != 0 {
		for i := range sizes {
			sizes[i] = sampleSize
		}
		return sizes, nil
	}
	b, err = readAt(r, box.start+box.headerSize+12, int(count)*4)
	if err != nil {
		return nil, err
	}
	for i := range sizes {
		sizes[i] = binary.BigEndian.Uint32(b[i*4 : i*4+4])
	}
	return sizes, nil
}

func parseSTCO(r io.ReaderAt, box boxHeader) ([]uint64, error) {
	b, err := readAt(r, box.start+box.headerSize, 8)
	if err != nil {
		return nil, err
	}
	count := binary.BigEndian.Uint32(b[4:8])
	b, err = readAt(r, box.start+box.headerSize+8, int(count)*4)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, int(count))
	for i := range out {
		out[i] = uint64(binary.BigEndian.Uint32(b[i*4 : i*4+4]))
	}
	return out, nil
}

func parseCO64(r io.ReaderAt, box boxHeader) ([]uint64, error) {
	b, err := readAt(r, box.start+box.headerSize, 8)
	if err != nil {
		return nil, err
	}
	count := binary.BigEndian.Uint32(b[4:8])
	b, err = readAt(r, box.start+box.headerSize+8, int(count)*8)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, int(count))
	for i := range out {
		out[i] = binary.BigEndian.Uint64(b[i*8 : i*8+8])
	}
	return out, nil
}

func parseSTSC(r io.ReaderAt, box boxHeader) ([]stscEntry, error) {
	b, err := readAt(r, box.start+box.headerSize, 8)
	if err != nil {
		return nil, err
	}
	count := binary.BigEndian.Uint32(b[4:8])
	b, err = readAt(r, box.start+box.headerSize+8, int(count)*12)
	if err != nil {
		return nil, err
	}
	out := make([]stscEntry, int(count))
	for i := range out {
		base := i * 12
		out[i] = stscEntry{
			FirstChunk:             binary.BigEndian.Uint32(b[base : base+4]),
			SamplesPerChunk:        binary.BigEndian.Uint32(b[base+4 : base+8]),
			SampleDescriptionIndex: binary.BigEndian.Uint32(b[base+8 : base+12]),
		}
	}
	return out, nil
}

func parseSTTS(r io.ReaderAt, box boxHeader) ([]sttsEntry, error) {
	b, err := readAt(r, box.start+box.headerSize, 8)
	if err != nil {
		return nil, err
	}
	count := binary.BigEndian.Uint32(b[4:8])
	b, err = readAt(r, box.start+box.headerSize+8, int(count)*8)
	if err != nil {
		return nil, err
	}
	out := make([]sttsEntry, int(count))
	for i := range out {
		base := i * 8
		out[i] = sttsEntry{
			SampleCount: binary.BigEndian.Uint32(b[base : base+4]),
			SampleDelta: binary.BigEndian.Uint32(b[base+4 : base+8]),
		}
	}
	return out, nil
}

func extractGPSPoints(r io.ReaderAt, t *track, start time.Time) ([]gpsPoint, error) {
	refs, err := t.samples()
	if err != nil {
		return nil, err
	}
	points := make([]gpsPoint, 0, len(refs))
	for _, ref := range refs {
		payload, err := readAt(r, ref.Offset, int(ref.Size))
		if err != nil {
			return nil, err
		}
		payload = unwrapEmbeddedMP4(payload)
		point, ok := extractGPSFromProto(payload)
		if !ok {
			continue
		}
		point.Time = start.Add(time.Duration(ref.Time * float64(time.Second)))
		points = append(points, point)
	}
	return points, nil
}

func extractGPSFromProto(b []byte) (gpsPoint, bool) {
	fields, ok := parseProtoFields(b)
	if !ok {
		return gpsPoint{}, false
	}
	return findGPSInFields(fields, 0)
}

func findGPSInFields(fields []protoField, depth int) (gpsPoint, bool) {
	if point, ok := gpsFromTelemetryMessage(fields); ok {
		return point, true
	}
	if depth >= 8 {
		return gpsPoint{}, false
	}
	for _, field := range fields {
		if field.Wire != 2 || len(field.Bytes) == 0 {
			continue
		}
		children, ok := parseProtoFields(field.Bytes)
		if !ok {
			continue
		}
		if point, ok := findGPSInFields(children, depth+1); ok {
			return point, true
		}
	}
	return gpsPoint{}, false
}

func gpsFromTelemetryMessage(fields []protoField) (gpsPoint, bool) {
	var location *protoField
	var relAlt *protoField
	for i := range fields {
		switch fields[i].Number {
		case 4:
			location = &fields[i]
		case 5:
			relAlt = &fields[i]
		}
	}
	if location == nil || location.Wire != 2 {
		return gpsPoint{}, false
	}
	locFields, ok := parseProtoFields(location.Bytes)
	if !ok {
		return gpsPoint{}, false
	}

	var latLonField *protoField
	var absAltMM uint64
	var hasAbs bool
	for i := range locFields {
		switch locFields[i].Number {
		case 1:
			latLonField = &locFields[i]
		case 2:
			if locFields[i].Wire == 0 {
				absAltMM = locFields[i].Uint
				hasAbs = true
			}
		}
	}
	if latLonField == nil || latLonField.Wire != 2 || !hasAbs {
		return gpsPoint{}, false
	}
	coordFields, ok := parseProtoFields(latLonField.Bytes)
	if !ok {
		return gpsPoint{}, false
	}

	var lat, lon float64
	var hasLat, hasLon bool
	for _, f := range coordFields {
		if f.Wire != 1 {
			continue
		}
		switch f.Number {
		case 2:
			lat = math.Float64frombits(f.Fixed64)
			hasLat = true
		case 3:
			lon = math.Float64frombits(f.Fixed64)
			hasLon = true
		}
	}
	if !hasLat || !hasLon || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return gpsPoint{}, false
	}

	point := gpsPoint{
		Lat:    lat,
		Lon:    lon,
		AbsAlt: float64(absAltMM) / 1000.0,
	}
	if relAlt != nil && relAlt.Wire == 2 {
		if relFields, ok := parseProtoFields(relAlt.Bytes); ok {
			for _, f := range relFields {
				if f.Number == 1 && f.Wire == 5 {
					point.RelAlt = float64(math.Float32frombits(f.Fixed32)) / 1000.0
				}
			}
		}
	}
	return point, true
}

func parseProtoFields(b []byte) ([]protoField, bool) {
	var fields []protoField
	for pos := 0; pos < len(b); {
		key, n := readVarint(b[pos:])
		if n <= 0 {
			return fields, false
		}
		pos += n
		fieldNo := key >> 3
		wire := key & 7
		if fieldNo == 0 {
			return fields, false
		}
		field := protoField{Number: fieldNo, Wire: wire}
		switch wire {
		case 0:
			v, m := readVarint(b[pos:])
			if m <= 0 {
				return fields, false
			}
			field.Uint = v
			pos += m
		case 1:
			if pos+8 > len(b) {
				return fields, false
			}
			field.Fixed64 = binary.LittleEndian.Uint64(b[pos : pos+8])
			pos += 8
		case 2:
			length, m := readVarint(b[pos:])
			if m <= 0 || length > uint64(len(b)-pos-m) {
				return fields, false
			}
			pos += m
			field.Bytes = b[pos : pos+int(length)]
			pos += int(length)
		case 5:
			if pos+4 > len(b) {
				return fields, false
			}
			field.Fixed32 = binary.LittleEndian.Uint32(b[pos : pos+4])
			pos += 4
		default:
			return fields, false
		}
		fields = append(fields, field)
	}
	return fields, true
}

func writeGPX(w io.Writer, points []gpsPoint, name string) error {
	if _, err := fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `<gpx version="1.1" creator="osv2gpx" xmlns="http://www.topografix.com/GPX/1/1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.topografix.com/GPX/1/1 http://www.topografix.com/GPX/1/1/gpx.xsd">`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  <trk>"); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "    <name>"); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(name)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "</name>"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    <trkseg>"); err != nil {
		return err
	}
	for _, p := range points {
		if _, err := fmt.Fprintf(w, "      <trkpt lat=\"%.8f\" lon=\"%.8f\">\n", p.Lat, p.Lon); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        <ele>%.3f</ele>\n", p.AbsAlt); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        <time>%s</time>\n", p.Time.UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "      </trkpt>"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, "    </trkseg>\n  </trk>\n</gpx>")
	return err
}

func setMP4CreationTimeFromGPX(gpxPath, mp4Path string) error {
	t, err := firstGPXTime(gpxPath)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(mp4Path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	updated, err := writeMP4CreationTime(f, t)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "set %s creation_time to %s from %s (%d fields updated)\n", mp4Path, t.UTC().Format(time.RFC3339), gpxPath, updated)
	return nil
}

func writeMP4CreationTime(f *os.File, t time.Time) (int, error) {
	size, err := readerSize(f)
	if err != nil {
		return 0, err
	}
	seconds, err := quickTimeSeconds(t)
	if err != nil {
		return 0, err
	}
	updated, err := patchMP4TimeBoxes(f, 0, size, seconds)
	if err != nil {
		return updated, err
	}
	if updated == 0 {
		return 0, errors.New("no mvhd/tkhd/mdhd time fields found")
	}
	return updated, nil
}

func patchMP4TimeBoxes(f *os.File, start, end int64, seconds uint64) (int, error) {
	updated := 0
	for _, box := range readBoxes(f, start, end) {
		switch box.typ {
		case "mvhd", "tkhd", "mdhd":
			n, err := patchFullBoxTimes(f, box, seconds)
			if err != nil {
				return updated, err
			}
			updated += n
		case "moov", "trak", "mdia":
			n, err := patchMP4TimeBoxes(f, box.start+box.headerSize, box.end, seconds)
			if err != nil {
				return updated, err
			}
			updated += n
		}
	}
	return updated, nil
}

func patchFullBoxTimes(f *os.File, box boxHeader, seconds uint64) (int, error) {
	b, err := readAt(f, box.start+box.headerSize, 1)
	if err != nil {
		return 0, err
	}
	base := box.start + box.headerSize
	if b[0] == 1 {
		if err := writeUint64At(f, base+4, seconds); err != nil {
			return 0, err
		}
		if err := writeUint64At(f, base+12, seconds); err != nil {
			return 0, err
		}
		return 2, nil
	}
	if seconds > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%s uses 32-bit time fields but %d exceeds uint32", box.typ, seconds)
	}
	if err := writeUint32At(f, base+4, uint32(seconds)); err != nil {
		return 0, err
	}
	if err := writeUint32At(f, base+8, uint32(seconds)); err != nil {
		return 0, err
	}
	return 2, nil
}

func quickTimeSeconds(t time.Time) (uint64, error) {
	quickTimeEpoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	t = t.UTC().Truncate(time.Second)
	if t.Before(quickTimeEpoch) {
		return 0, fmt.Errorf("time %s is before QuickTime epoch", t.Format(time.RFC3339))
	}
	return uint64(t.Sub(quickTimeEpoch) / time.Second), nil
}

func writeUint32At(f *os.File, off int64, v uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	_, err := f.WriteAt(b[:], off)
	return err
}

func writeUint64At(f *os.File, off int64, v uint64) error {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, err := f.WriteAt(b[:], off)
	return err
}

func firstGPXTime(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return time.Time{}, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "time" {
			continue
		}
		var value string
		if err := dec.DecodeElement(&value, &start); err != nil {
			return time.Time{}, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid GPX time %q: %w", value, err)
		}
		return parsed, nil
	}
	return time.Time{}, errors.New("no GPX time element found")
}

func unwrapEmbeddedMP4(b []byte) []byte {
	if len(b) < 16 || string(b[4:8]) != "ftyp" {
		return b
	}
	for off := int64(0); off+8 <= int64(len(b)); {
		size32 := binary.BigEndian.Uint32(b[off : off+4])
		typ := string(b[off+4 : off+8])
		headerSize := int64(8)
		size := int64(size32)
		if size32 == 1 {
			if off+16 > int64(len(b)) {
				return b
			}
			size = int64(binary.BigEndian.Uint64(b[off+8 : off+16]))
			headerSize = 16
		} else if size32 == 0 {
			size = int64(len(b)) - off
		}
		if size < headerSize || off+size > int64(len(b)) {
			return b
		}
		if typ == "mdat" {
			return b[off+headerSize : off+size]
		}
		off += size
	}
	return b
}

func readVarint(b []byte) (uint64, int) {
	var x uint64
	for i := 0; i < len(b) && i < 10; i++ {
		x |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return x, i + 1
		}
	}
	return 0, -1
}

func readAt(r io.ReaderAt, off int64, n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := r.ReadAt(b, off)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func readerSize(r io.ReaderAt) (int64, error) {
	if f, ok := r.(*os.File); ok {
		st, err := f.Stat()
		if err != nil {
			return 0, err
		}
		return st.Size(), nil
	}
	return 0, errors.New("reader size unavailable")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
