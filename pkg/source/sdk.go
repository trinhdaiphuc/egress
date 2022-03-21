package source

import (
	"sync"

	"github.com/go-logr/logr"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"go.uber.org/atomic"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"

	"github.com/livekit/livekit-egress/pkg/errors"
	"github.com/livekit/livekit-egress/pkg/pipeline/params"
)

const (
	maxVideoLate = 1000 // nearly 2s for fhd video
	maxAudioLate = 200  // 4s for audio
)

type SDKSource struct {
	mu sync.Mutex

	room      *lksdk.Room
	trackIDs  []string
	active    atomic.Int32
	recorders map[string]*trackRecorder

	endRecording chan struct{}

	logger logger.Logger
}

func NewSDKSource(p *params.Params, createWriter func(*webrtc.TrackRemote) (media.Writer, error)) (*SDKSource, error) {
	s := &SDKSource{
		room:         lksdk.CreateRoom(p.LKUrl),
		endRecording: make(chan struct{}),
		logger:       p.Logger,
		recorders:    make(map[string]*trackRecorder),
	}

	switch p.Info.Request.(type) {
	case *livekit.EgressInfo_TrackComposite:
		s.trackIDs = []string{p.AudioTrackID, p.VideoTrackID}
	default:
		s.trackIDs = []string{p.TrackID}
	}

	s.room.Callback.OnTrackSubscribed = func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		sb := createSampleBuilder(track.Codec(), samplebuilder.WithPacketDroppedHandler(func() {
			rp.WritePLI(track.SSRC())
		}))

		mw, err := createWriter(track)
		if err != nil {
			s.logger.Errorw("could not record track", err)
			return
		}

		tr := createTrackRecorder(sb, mw, logger.Logger(logr.Logger(p.Logger).WithValues("trackID", track.ID())))
		go tr.Start(track)

		s.mu.Lock()
		s.recorders[track.ID()] = tr
		s.mu.Unlock()
	}
	s.room.Callback.OnTrackUnpublished = s.onTrackUnpublished
	s.room.Callback.OnDisconnected = s.onComplete

	s.logger.Debugw("connecting to room")
	if err := s.room.JoinWithToken(p.LKUrl, p.Token); err != nil {
		return nil, err
	}

	if err := s.subscribeToTracks(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *SDKSource) subscribeToTracks() error {
	expecting := make(map[string]bool)
	for _, trackID := range s.trackIDs {
		expecting[trackID] = true
	}

	for _, p := range s.room.GetParticipants() {
		for _, track := range p.Tracks() {
			if expecting[track.SID()] {
				if rt, ok := track.(*lksdk.RemoteTrackPublication); ok {
					err := rt.SetSubscribed(true)
					if err != nil {
						return err
					}

					delete(expecting, track.SID())
					s.active.Inc()
					if len(expecting) == 0 {
						return nil
					}
				}
			}
		}
	}

	for trackID := range expecting {
		return errors.ErrTrackNotFound(trackID)
	}

	return nil
}

func (s *SDKSource) onTrackUnpublished(track *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
	for _, trackID := range s.trackIDs {
		if track.SID() == trackID {
			if s.active.Dec() == 0 {
				s.onComplete()
			}
			return
		}
	}
}

func (s *SDKSource) onComplete() {
	select {
	case <-s.endRecording:
		return
	default:
		close(s.endRecording)
	}
}

func (s *SDKSource) StartRecording() chan struct{} {
	start := make(chan struct{})
	close(start)
	return start
}

func (s *SDKSource) EndRecording() chan struct{} {
	return s.endRecording
}

func (s *SDKSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.recorders {
		r.Stop()
	}
	s.room.Disconnect()
}
