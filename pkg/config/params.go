package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/livekit/livekit-egress/pkg/errors"
)

type Params struct {
	// source
	LKApiKey     string
	LKApiSecret  string
	LKUrl        string
	RoomName     string
	TemplateBase string

	// web source
	IsWebInput     bool
	Layout         string
	CustomBase     string
	CustomInputURL string

	// sdk source
	TrackID      string
	AudioTrackID string
	VideoTrackID string

	// audio
	AudioEnabled   bool
	AudioCodec     livekit.AudioCodec
	AudioBitrate   int32
	AudioFrequency int32

	// video
	VideoEnabled bool
	VideoCodec   livekit.VideoCodec
	Width        int32
	Height       int32
	Depth        int32
	Framerate    int32
	VideoBitrate int32

	// format
	IsStream       bool
	StreamProtocol livekit.StreamProtocol
	FileType       livekit.EncodedFileType
	StreamUrls     []string

	// info
	Info       *livekit.EgressInfo
	FileInfo   *livekit.FileInfo
	StreamInfo map[string]*livekit.StreamInfo
}

var (
	hd30 = Params{
		AudioEnabled:   true,
		AudioCodec:     livekit.AudioCodec_DEFAULT_AC,
		AudioBitrate:   128,
		AudioFrequency: 44100,
		VideoEnabled:   true,
		VideoCodec:     livekit.VideoCodec_DEFAULT_VC,
		Width:          1280,
		Height:         720,
		Depth:          24,
		Framerate:      30,
		VideoBitrate:   3000,
	}

	hd60 = Params{
		AudioEnabled:   true,
		AudioCodec:     livekit.AudioCodec_DEFAULT_AC,
		AudioBitrate:   128,
		AudioFrequency: 44100,
		VideoEnabled:   true,
		VideoCodec:     livekit.VideoCodec_DEFAULT_VC,
		Width:          1280,
		Height:         720,
		Depth:          24,
		Framerate:      60,
		VideoBitrate:   4500,
	}

	fullHD30 = Params{
		AudioEnabled:   true,
		AudioCodec:     livekit.AudioCodec_DEFAULT_AC,
		AudioBitrate:   128,
		AudioFrequency: 44100,
		VideoEnabled:   true,
		VideoCodec:     livekit.VideoCodec_DEFAULT_VC,
		Width:          1920,
		Height:         1080,
		Depth:          24,
		Framerate:      30,
		VideoBitrate:   4500,
	}

	fullHD60 = Params{
		AudioEnabled:   true,
		AudioCodec:     livekit.AudioCodec_DEFAULT_AC,
		AudioBitrate:   128,
		AudioFrequency: 44100,
		VideoEnabled:   true,
		VideoCodec:     livekit.VideoCodec_DEFAULT_VC,
		Width:          1920,
		Height:         1080,
		Depth:          24,
		Framerate:      60,
		VideoBitrate:   6000,
	}

	mp4 = livekit.EncodedFileType_MP4.String()
	// webm = livekit.EncodedFileType_WEBM.String()
	ogg  = livekit.EncodedFileType_OGG.String()
	rtmp = livekit.StreamProtocol_RTMP.String()
	// srt  = livekit.StreamProtocol_SRT.String()

	DefaultAudioCodecs = map[string]livekit.AudioCodec{
		mp4: livekit.AudioCodec_AAC,
		// webm: livekit.AudioCodec_OPUS,
		ogg:  livekit.AudioCodec_OPUS,
		rtmp: livekit.AudioCodec_AAC,
		// srt:  livekit.AudioCodec(-1), // unknown
	}

	DefaultVideoCodecs = map[string]livekit.VideoCodec{
		mp4: livekit.VideoCodec_H264_MAIN,
		// webm: livekit.VideoCodec_VP8,
		// ogg:  livekit.VideoCodec_VP8,
		rtmp: livekit.VideoCodec_H264_MAIN,
		// srt:  livekit.VideoCodec(-1), // unknown
	}

	compatibleAudioCodecs = map[string]map[livekit.AudioCodec]bool{
		mp4: {
			livekit.AudioCodec_AAC:  true,
			livekit.AudioCodec_OPUS: true,
		},
		// webm: {
		// 	livekit.AudioCodec_OPUS: true,
		// },
		ogg: {
			livekit.AudioCodec_OPUS: true,
		},
		rtmp: {
			livekit.AudioCodec_AAC: true,
		},
		// srt: {
		// 	unknown
		// },
	}

	compatibleVideoCodecs = map[string]map[livekit.VideoCodec]bool{
		mp4: {
			livekit.VideoCodec_H264_BASELINE: true,
			livekit.VideoCodec_H264_MAIN:     true,
			livekit.VideoCodec_H264_HIGH:     true,
			// livekit.VideoCodec_HEVC_MAIN:     true,
			// livekit.VideoCodec_HEVC_HIGH:     true,
		},
		// webm: {
		// 	livekit.VideoCodec_VP8: true,
		// 	livekit.VideoCodec_VP9: true,
		// },
		ogg: {
			// livekit.VideoCodec_VP8: true,
		},
		rtmp: {
			livekit.VideoCodec_H264_BASELINE: true,
			livekit.VideoCodec_H264_MAIN:     true,
			livekit.VideoCodec_H264_HIGH:     true,
		},
		// srt: {
		// 	unknown
		// },
	}
)

func GetPipelineParams(conf *Config, request *livekit.StartEgressRequest) (*Params, error) {
	params := getEncodingParams(request)
	params.Info = &livekit.EgressInfo{
		EgressId: request.EgressId,
		RoomId:   request.RoomId,
		Status:   livekit.EgressStatus_EGRESS_STARTING,
	}

	if request.ApiKey != "" && request.ApiSecret != "" && request.WsUrl != "" {
		params.LKApiKey = request.ApiKey
		params.LKApiSecret = request.ApiSecret
		params.LKUrl = request.WsUrl
	} else {
		params.LKApiKey = conf.ApiKey
		params.LKApiSecret = conf.ApiSecret
		params.LKUrl = conf.WsUrl
	}

	var format string
	switch req := request.Request.(type) {
	case *livekit.StartEgressRequest_WebComposite:
		params.Info.Request = &livekit.EgressInfo_WebComposite{WebComposite: req.WebComposite}

		params.AudioEnabled = !req.WebComposite.VideoOnly
		params.VideoEnabled = !req.WebComposite.AudioOnly
		params.IsWebInput = true
		params.Layout = req.WebComposite.Layout
		params.RoomName = req.WebComposite.RoomName
		if req.WebComposite.CustomBaseUrl != "" {
			params.TemplateBase = req.WebComposite.CustomBaseUrl
		} else {
			params.TemplateBase = conf.TemplateBase
		}

		switch o := req.WebComposite.Output.(type) {
		case *livekit.WebCompositeEgressRequest_File:
			format = o.File.FileType.String()
			if err := params.updateFileInfo(o.File.FileType, o.File.HttpUrl); err != nil {
				return nil, err
			}
		case *livekit.WebCompositeEgressRequest_Stream:
			format = o.Stream.Protocol.String()
			if err := params.updateStreamInfo(o.Stream.Protocol, o.Stream.Urls); err != nil {
				return nil, err
			}
		default:
			return nil, errors.ErrInvalidInput
		}
	case *livekit.StartEgressRequest_TrackComposite:
		params.Info.Request = &livekit.EgressInfo_TrackComposite{TrackComposite: req.TrackComposite}

		params.AudioEnabled = req.TrackComposite.AudioTrackId != ""
		params.VideoEnabled = req.TrackComposite.VideoTrackId != ""
		params.RoomName = req.TrackComposite.RoomName

		switch o := req.TrackComposite.Output.(type) {
		case *livekit.TrackCompositeEgressRequest_File:
			format = o.File.FileType.String()
			if err := params.updateFileInfo(o.File.FileType, o.File.HttpUrl); err != nil {
				return nil, err
			}
		case *livekit.TrackCompositeEgressRequest_Stream:
			format = o.Stream.Protocol.String()
			if err := params.updateStreamInfo(o.Stream.Protocol, o.Stream.Urls); err != nil {
				return nil, err
			}
		}

		return nil, errors.ErrNotSupported("track composite requests")
	case *livekit.StartEgressRequest_Track:
		params.Info.Request = &livekit.EgressInfo_Track{Track: req.Track}

		params.RoomName = req.Track.RoomName
		params.TrackID = req.Track.TrackId

		// switch o := req.Track.Output.(type) {
		// case *livekit.TrackEgressRequest_HttpUrl:
		// case *livekit.TrackEgressRequest_WebsocketUrl:
		// default:
		// 	return nil, errors.ErrInvalidInput
		// }

		return nil, errors.ErrNotSupported("track requests")
		// return params, nil
	default:
		return nil, errors.ErrInvalidInput
	}

	// check audio codec
	if params.AudioEnabled {
		if params.AudioCodec == livekit.AudioCodec_DEFAULT_AC {
			params.AudioCodec = DefaultAudioCodecs[format]
		} else if !compatibleAudioCodecs[format][params.AudioCodec] {
			return nil, errors.ErrIncompatible(format, params.AudioCodec)
		}
	}

	// check video codec
	if params.VideoEnabled {
		if params.VideoCodec == livekit.VideoCodec_DEFAULT_VC {
			params.VideoCodec = DefaultVideoCodecs[format]
		} else if !compatibleVideoCodecs[format][params.VideoCodec] {
			return nil, errors.ErrIncompatible(format, params.VideoCodec)
		}
	}

	return params, nil
}

func getEncodingParams(request *livekit.StartEgressRequest) *Params {
	var preset livekit.EncodingOptionsPreset = -1
	var advanced *livekit.EncodingOptions

	switch req := request.Request.(type) {
	case *livekit.StartEgressRequest_WebComposite:
		switch opts := req.WebComposite.Options.(type) {
		case *livekit.WebCompositeEgressRequest_Preset:
			preset = opts.Preset
		case *livekit.WebCompositeEgressRequest_Advanced:
			advanced = opts.Advanced
		}
	case *livekit.StartEgressRequest_TrackComposite:
		switch options := req.TrackComposite.Options.(type) {
		case *livekit.TrackCompositeEgressRequest_Preset:
			preset = options.Preset
		case *livekit.TrackCompositeEgressRequest_Advanced:
			advanced = options.Advanced
		}
	}

	params := fullHD30
	if preset != -1 {
		switch preset {
		case livekit.EncodingOptionsPreset_H264_720P_30:
			params = hd30
		case livekit.EncodingOptionsPreset_H264_720P_60:
			params = hd60
		case livekit.EncodingOptionsPreset_H264_1080P_30:
			// default
		case livekit.EncodingOptionsPreset_H264_1080P_60:
			params = fullHD60
		}
	} else if advanced != nil {
		// audio
		params.AudioCodec = advanced.AudioCodec
		if advanced.AudioBitrate != 0 {
			params.AudioBitrate = advanced.AudioBitrate
		}
		if advanced.AudioFrequency != 0 {
			params.AudioFrequency = advanced.AudioFrequency
		}

		// video
		params.VideoCodec = advanced.VideoCodec
		if advanced.Width != 0 {
			params.Width = advanced.Width
		}
		if advanced.Height != 0 {
			params.Height = advanced.Height
		}
		if advanced.Depth != 0 {
			params.Depth = advanced.Depth
		}
		if advanced.Framerate != 0 {
			params.Framerate = advanced.Framerate
		}
		if advanced.VideoBitrate != 0 {
			params.VideoBitrate = advanced.VideoBitrate
		}
	}

	return &params
}

func (p *Params) updateStreamInfo(protocol livekit.StreamProtocol, urls []string) error {
	p.IsStream = true
	p.StreamProtocol = protocol
	p.StreamUrls = urls

	p.StreamInfo = make(map[string]*livekit.StreamInfo)
	var streamInfoList []*livekit.StreamInfo
	for _, url := range urls {
		switch protocol {
		case livekit.StreamProtocol_RTMP:
			if !strings.HasPrefix(url, "rtmp://") {
				return errors.ErrInvalidURL
			}
		}

		info := &livekit.StreamInfo{
			Url: url,
		}
		p.StreamInfo[url] = info
		streamInfoList = append(streamInfoList, info)
	}
	p.Info.Result = &livekit.EgressInfo_Stream{Stream: &livekit.StreamInfoList{Info: streamInfoList}}
	return nil
}

func (p *Params) updateFileInfo(fileType livekit.EncodedFileType, fileUrl string) error {
	p.FileType = fileType
	filename, err := getFileName(fileUrl, fileType, p.RoomName)
	if err != nil {
		return err
	}
	p.FileInfo = &livekit.FileInfo{
		Filename: filename,
		Location: fileUrl,
	}
	p.Info.Result = &livekit.EgressInfo_File{File: p.FileInfo}
	return nil
}

func getFileName(fileUrl string, fileType livekit.EncodedFileType, roomName string) (string, error) {
	if strings.Contains(fileUrl, "://") {
		return fmt.Sprintf("%s-%v.%s",
			roomName,
			time.Now().String(),
			strings.ToLower(fileType.String()),
		), nil
	}

	filename := fileUrl
	if idx := strings.LastIndex(filename, "/"); idx != -1 {
		if err := os.MkdirAll(filename[:idx], os.ModeDir); err != nil {
			return "", err
		}
	}

	ext := "." + strings.ToLower(fileType.String())
	if !strings.HasSuffix(filename, ext) {
		filename = filename + ext
	}

	return filename, nil
}
