package source

import (
	"context"
	"github.com/livekit/livekit-egress/pkg/errors"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264writer"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"io"
)

type pionWriter struct {
	ctx    context.Context
	cancel context.CancelFunc
	sb     *samplebuilder.SampleBuilder
	mw     media.Writer
	track  *webrtc.TrackRemote

	sink   io.WriteCloser
	logger logger.Logger
}

func createSampleBuilder(codec webrtc.RTPCodecParameters, opts ...samplebuilder.Option) *samplebuilder.SampleBuilder {
	switch codec.MimeType {
	case webrtc.MimeTypeVP8:
		return samplebuilder.New(maxVideoLate, &codecs.VP8Packet{}, codec.ClockRate, opts...)
	case webrtc.MimeTypeH264:
		return samplebuilder.New(maxVideoLate, &codecs.H264Packet{}, codec.ClockRate, opts...)
	case webrtc.MimeTypeOpus:
		return samplebuilder.New(maxVideoLate, &codecs.OpusPacket{}, codec.ClockRate, opts...)
	}
	return nil
}

func createMediaWriter(codec webrtc.RTPCodecParameters, sink io.WriteCloser) (media.Writer, error) {
	switch codec.MimeType {
	case webrtc.MimeTypeVP8:
		return ivfwriter.NewWith(sink)
	case webrtc.MimeTypeH264:
		return h264writer.NewWith(sink), nil
	case webrtc.MimeTypeOpus:
		return oggwriter.NewWith(sink, codec.ClockRate, codec.Channels)
	}
	return nil, errors.ErrNotSupported(codec.MimeType)
}

func newPionWriter(track *webrtc.TrackRemote, pli lksdk.PLIWriter, sink io.WriteCloser, logger logger.Logger) (*pionWriter, error) {
	w := &pionWriter{
		track:  track,
		sink:   sink,
		logger: logger,
	}
	w.ctx, w.cancel = context.WithCancel(context.TODO())

	var err error
	w.sb = createSampleBuilder(track.Codec(), samplebuilder.WithPacketDroppedHandler(func() {
		pli(track.SSRC())
	}))
	w.mw, err = createMediaWriter(track.Codec(), sink)
	if err != nil {
		return nil, err
	}

	go w.start()
	return w, nil
}

func (w *pionWriter) start() {
	var err error
	defer func() {
		if err != nil {
			w.logger.Errorw("error writing to sink", err)
		}

		if err = w.mw.Close(); err != nil {
			w.logger.Errorw("error closing sink", err)
		}
	}()

	var pkt *rtp.Packet
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			// Extract RTP packet
			pkt, _, err = w.track.ReadRTP()
			if err != nil {
				return
			}

			// Write to sample builder and see if we have packets ready to
			// be written to sink
			w.sb.Push(pkt)
			for _, p := range w.sb.PopPackets() {
				if err = w.mw.WriteRTP(p); err != nil {
					return
				}
			}
		}
	}
}

func (w *pionWriter) stop() {
	w.cancel()
}
