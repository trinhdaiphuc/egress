package source

import (
	"bytes"
	"context"
	
	"github.com/gorilla/websocket"

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
)

type wsWriter struct {
	ctx    context.Context
	cancel context.CancelFunc

	conn  *websocket.Conn
	sb    *samplebuilder.SampleBuilder
	mw    media.Writer
	buf   *bytes.Buffer
	track *webrtc.TrackRemote

	logger logger.Logger
}

func newWsWriter(track *webrtc.TrackRemote, pli lksdk.PLIWriter, url string, logger logger.Logger) (*wsWriter, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	w := &wsWriter{
		conn:   conn,
		buf:    &bytes.Buffer{},
		track:  track,
		logger: logger,
	}
	w.ctx, w.cancel = context.WithCancel(context.TODO())

	switch track.Codec().MimeType {
	case webrtc.MimeTypeVP8:
		w.sb = samplebuilder.New(maxVideoLate, &codecs.VP8Packet{}, track.Codec().ClockRate,
			samplebuilder.WithPacketDroppedHandler(func() { pli(track.SSRC()) }))
		w.mw, err = ivfwriter.NewWith(w.buf)
	case webrtc.MimeTypeH264:
		w.sb = samplebuilder.New(maxVideoLate, &codecs.H264Packet{}, track.Codec().ClockRate,
			samplebuilder.WithPacketDroppedHandler(func() { pli(track.SSRC()) }))
		w.mw = h264writer.NewWith(w.buf)
	case webrtc.MimeTypeOpus:
		w.sb = samplebuilder.New(maxAudioLate, &codecs.OpusPacket{}, track.Codec().ClockRate,
			samplebuilder.WithPacketDroppedHandler(func() { pli(track.SSRC()) }))
		w.mw, err = oggwriter.NewWith(w.buf, track.Codec().ClockRate, track.Codec().Channels)
	default:
		return nil, errors.ErrNotSupported(track.Codec().MimeType)
	}
	if err != nil {
		return nil, err
	}
	go w.start()
	return w, nil
}

func (w *wsWriter) start() {
	var err error
	defer func() {
		if err != nil {
			w.logger.Errorw("error writing to ws", err)
		}

		if err = w.mw.Close(); err != nil {
			w.logger.Errorw("error closing buffer", err)
		}

		if err = w.conn.Close(); err != nil {
			w.logger.Errorw("error closing socket", err)
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

			// Write to buffer
			w.sb.Push(pkt)
			for _, p := range w.sb.PopPackets() {
				if err = w.mw.WriteRTP(p); err != nil {
					w.logger.Errorw("could not write to file", err)
					return
				}
			}

			// From buffer, we write to socket
			err = w.conn.WriteMessage(websocket.BinaryMessage, w.buf.Bytes())
			if err != nil {
				return
			}
		}
	}
}

func (w *wsWriter) stop() {
	w.cancel()
}
