package flac

import (
	"fmt"
	"io"

	meta "github.com/go-flac/go-flac"
	flac "github.com/go-musicfox/goflac"
	"github.com/ikemen-engine/beep"
	"github.com/pkg/errors"
)

// Decode takes a Reader containing audio data in FLAC format and returns a StreamSeekCloser,
// which streams that audio. The Seek method will panic if r is not io.Seeker.
//
// Do not close the supplied Reader, instead, use the Close method of the returned
// StreamSeekCloser when you want to release the resources.
func Decode(rc io.ReadSeekCloser) (s beep.StreamSeekCloser, format beep.Format, err error) {
	d := decoder{r: rc}
	defer func() { // hacky way to always close r if an error occurred
		if closer, ok := d.r.(io.Closer); ok {
			if err != nil {
				closer.Close()
				err = errors.Wrap(err, "flac")
			}
		}
	}()

	// First we need the metadata (just to get the # of samples)
	var f *meta.File
	if f, err = meta.ParseMetadata(rc); err != nil {
		return nil, beep.Format{}, errors.Wrap(err, "flac-meta")
	}
	var streaminfo *meta.StreamInfoBlock
	if streaminfo, err = f.GetStreamInfo(); err != nil {
		return nil, beep.Format{}, errors.Wrap(err, "flac-meta")
	}
	d.len = streaminfo.SampleCount

	// Seek to 0 so the decoder doesn't freak out.
	rc.Seek(0, io.SeekStart)
	if d.stream, err = flac.NewDecoderReader(rc); err != nil {
		return nil, beep.Format{}, errors.Wrap(err, "flac")
	}

	format = beep.Format{
		SampleRate:  beep.SampleRate(d.stream.Rate),
		NumChannels: d.stream.Channels,
		Precision:   d.stream.Depth / 8,
	}

	return &d, format, nil
}

type decoder struct {
	r      io.Reader
	stream *flac.Decoder
	buf    [][2]float64
	pos    int
	err    error
	len    int64
}

func (d *decoder) Stream(samples [][2]float64) (n int, ok bool) {
	if d.err != nil {
		return 0, false
	}
	// Copy samples from buffer.
	j := 0
	for i := range samples {
		if j >= len(d.buf) {
			// refill buffer.
			if err := d.refill(); err != nil {
				// Only set the error if it's not EOF
				if err != io.EOF {
					d.err = err
					d.pos += n
				} else {
					// Set the pos to the end if less than length because we've reached EOF (hack to work with loop)
					if int64(d.pos) < d.len {
						d.pos = int(d.len)
					}
				}
				return n, n > 0
			}
			j = 0
		}
		samples[i] = d.buf[j]
		j++
		n++
	}
	d.buf = d.buf[j:]
	d.pos += n
	return n, true
}

// refill decodes audio samples to fill the decode buffer.
func (d *decoder) refill() error {
	// Empty buffer.
	d.buf = d.buf[:0]
	// Parse audio frame.
	frame, err := d.stream.ReadFrame()
	if err != nil {
		return err
	}

	// Decode audio samples.
	bps := d.stream.Depth
	nchannels := d.stream.Channels
	// Expand buffer size if needed.
	n := len(frame.Buffer) / nchannels
	if cap(d.buf) < n {
		d.buf = make([][2]float64, n)
	} else {
		d.buf = d.buf[:n]
	}
	s := 1 << (bps - 1)
	q := 1 / float64(s)
	switch {
	case bps == 8 && nchannels == 1:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(int8(frame.Buffer[i])) * q
			d.buf[i][1] = float64(int8(frame.Buffer[i])) * q
		}
	case bps == 16 && nchannels == 1:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(int16(frame.Buffer[i])) * q
			d.buf[i][1] = float64(int16(frame.Buffer[i])) * q
		}
	case bps == 24 && nchannels == 1:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(frame.Buffer[i]) * q
			d.buf[i][1] = float64(frame.Buffer[i]) * q
		}
	case bps == 8 && nchannels >= 2:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(int8(frame.Buffer[i*nchannels])) * q
			d.buf[i][1] = float64(int8(frame.Buffer[i*nchannels+1])) * q
		}
	case bps == 16 && nchannels >= 2:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(int16(frame.Buffer[i*nchannels])) * q
			d.buf[i][1] = float64(int16(frame.Buffer[i*nchannels+1])) * q
		}
	case bps == 24 && nchannels >= 2:
		for i := 0; i < n; i++ {
			d.buf[i][0] = float64(frame.Buffer[i*nchannels]) * q
			d.buf[i][1] = float64(frame.Buffer[i*nchannels+1]) * q
		}
	default:
		panic(fmt.Errorf("support for %d bits-per-sample and %d channels combination not yet implemented", bps, nchannels))
	}
	// print("GET OUTTA HEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE")
	return nil
}

func (d *decoder) Err() error {
	return d.err
}

func (d *decoder) Len() int {
	return int(d.len)
}

func (d *decoder) Position() int {
	return int(d.pos)
}

// p represents flac sample num perhaps?
func (d *decoder) Seek(p int) error {
	pos, err := d.stream.Seek(uint64(p))
	d.pos = int(pos)
	return err
}

func (d *decoder) Close() error {
	if closer, ok := d.r.(io.Closer); ok {
		err := closer.Close()
		if err != nil {
			return errors.Wrap(err, "flac")
		}
	}
	return nil
}
