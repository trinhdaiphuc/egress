package source

import (
	"context"
	"strings"

	"github.com/livekit/protocol/logger"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pkg/errors"
)

type trackRecorder struct {
	sb     *samplebuilder.SampleBuilder
	mw     media.Writer
	logger logger.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

func createSampleBuilder(codec webrtc.RTPCodecParameters, opts ...samplebuilder.Option) *samplebuilder.SampleBuilder {
	switch {
	case strings.EqualFold(codec.MimeType, "audio/opus"):
		return samplebuilder.New(maxAudioLate, &codecs.OpusPacket{}, codec.ClockRate, opts...)
	case strings.EqualFold(codec.MimeType, "video/vp8"):
		return samplebuilder.New(maxVideoLate, &codecs.VP8Packet{}, codec.ClockRate, opts...)
	case strings.EqualFold(codec.MimeType, "video/h264"):
		return samplebuilder.New(maxVideoLate, &codecs.H264Packet{}, codec.ClockRate, opts...)
	default:
		return nil
	}
}

func createTrackRecorder(sb *samplebuilder.SampleBuilder, mw media.Writer, logger logger.Logger) *trackRecorder {
	ctx, cancel := context.WithCancel(context.TODO())
	return &trackRecorder{sb, mw, logger, ctx, cancel}
}

func (r *trackRecorder) Start(track *webrtc.TrackRemote) {
	var err error
	defer func() {
		if err != nil {
			r.logger.Errorw("error recording track", err)
		}

		err = r.mw.Close()
		if err != nil {
			r.logger.Errorw("could not close track writer", err)
		}
	}()

	var packet *rtp.Packet
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			packet, _, err = track.ReadRTP()
			if err != nil {
				err = errors.Wrap(err, "could not read from track")
				return
			}

			err = r.writeRTP(packet)
			if err != nil {
				err = errors.Wrap(err, "could not write to file")
				return
			}
		}
	}
}

func (r *trackRecorder) writeRTP(pkt *rtp.Packet) error {
	// If no sample buffer is specified, write immediately
	if r.sb == nil {
		return r.mw.WriteRTP(pkt)
	}

	// Else, push to buffer first
	r.sb.Push(pkt)

	// Check if we have packets ready to be written
	for _, p := range r.sb.PopPackets() {
		if err := r.mw.WriteRTP(p); err != nil {
			return err
		}
	}
	return nil
}

func (r *trackRecorder) Stop() {
	r.cancel()
}
