package tmlabseedance

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const taskRequestContextKey = "tmlab_seedance_task_request"

type mixedReference struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

// submitRequest contains the five request dialects currently used by the
// Seedance models on TMLab. Fields from unrelated dialects remain omitted.
type submitRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`

	// Stable mini/fast/pro request fields.
	ModeType    string           `json:"mode_type,omitempty"`
	Duration    *int             `json:"duration,omitempty"`
	Ratio       string           `json:"ratio,omitempty"`
	Resolution  string           `json:"resolution,omitempty"`
	EnableSound string           `json:"enable_sound,omitempty"`
	ImageURLs   []string         `json:"image_urls,omitempty"`
	AudioURLs   []string         `json:"audio_urls,omitempty"`
	VideoURLs   []string         `json:"video_urls,omitempty"`
	MixedList   []mixedReference `json:"mixed_list,omitempty"`

	// [V2] request fields.
	Images []string `json:"images,omitempty"`

	// Pro 720p and Seedance 2.5 request fields.
	DurationSec    *int     `json:"duration_sec,omitempty"`
	AspectRatio    string   `json:"aspect_ratio,omitempty"`
	ReferenceURL   string   `json:"reference_url,omitempty"`
	ReferenceURLs  []string `json:"reference_urls,omitempty"`
	EnableFaceMask *bool    `json:"enable_face_mask,omitempty"`
	InputImages    []string `json:"input_images,omitempty"`
	InputAudios    []string `json:"input_audios,omitempty"`

	// 431 request fields use upstream-defined camelCase names.
	FirstImage      string   `json:"first_image,omitempty"`
	LastImage       string   `json:"last_image,omitempty"`
	ReferenceImages []string `json:"referenceImages,omitempty"`
	ReferenceVideos []string `json:"referenceVideos,omitempty"`
	ReferenceAudios []string `json:"referenceAudios,omitempty"`
}

type taskResponse struct {
	ID            string `json:"id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Progress      any    `json:"progress,omitempty"`
	ResultURL     string `json:"result_url,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
	Error         any    `json:"error,omitempty"`
	Metadata      struct {
		URL string `json:"url,omitempty"`
	} `json:"metadata,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) PollingInterval(task *model.Task) time.Duration {
	modelName := task.Properties.UpstreamModelName
	if modelName == "" {
		modelName = task.Properties.OriginModelName
	}
	if modelName == ModelSeedanceFast431 || modelName == ModelSeedancePro431 {
		return 30 * time.Second
	}
	return 10 * time.Second
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return invalidRequest(err, "invalid_multipart_form")
		}
		if len(form.File["input_reference"]) > 0 {
			return invalidRequest(
				fmt.Errorf("TMLab Seedance requires input_reference to be a public URL; file upload is not supported"),
				"unsupported_input_reference_file",
			)
		}
	}

	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return invalidRequest(err, "invalid_request")
	}

	var request submitRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return invalidRequest(err, "invalid_request")
	}
	if taskReq.Metadata != nil {
		metadataBytes, err := common.Marshal(taskReq.Metadata)
		if err != nil {
			return invalidRequest(err, "invalid_metadata")
		}
		var metadataRequest submitRequest
		if err := common.Unmarshal(metadataBytes, &metadataRequest); err != nil {
			return invalidRequest(err, "invalid_metadata")
		}
		fillMissingRequestFields(&request, &metadataRequest)
	}

	request.Model = strings.TrimSpace(taskReq.Model)
	request.Prompt = strings.TrimSpace(taskReq.Prompt)
	if request.Duration == nil && taskReq.Duration > 0 {
		request.Duration = intPtr(taskReq.Duration)
	}
	if request.Duration == nil && taskReq.Seconds != "" {
		duration, err := strconv.Atoi(taskReq.Seconds)
		if err != nil {
			return invalidRequest(fmt.Errorf("seconds must be an integer"), "invalid_duration")
		}
		request.Duration = intPtr(duration)
	}

	validationModel := ""
	if info.ChannelMeta != nil {
		validationModel = strings.TrimSpace(info.UpstreamModelName)
	}
	if validationModel == "" {
		validationModel = request.Model
	}
	request.Model = validationModel

	profile, ok := modelProfiles[validationModel]
	if !ok {
		return invalidRequest(fmt.Errorf("unsupported TMLab Seedance model: %s", validationModel), "unsupported_model")
	}
	genericImages := taskReq.Images
	if len(genericImages) == 0 && strings.TrimSpace(taskReq.InputReference) != "" {
		genericImages = []string{strings.TrimSpace(taskReq.InputReference)}
	}
	if request.Ratio == "" && request.AspectRatio == "" && taskReq.Size != "" {
		switch strings.ToLower(strings.TrimSpace(taskReq.Size)) {
		case "1280x720":
			request.Ratio = "16:9"
		case "720x1280":
			request.Ratio = "9:16"
		case "1920x1080":
			request.Ratio = "16:9"
			request.Resolution = "1080P"
		case "1080x1920":
			request.Ratio = "9:16"
			request.Resolution = "1080P"
		}
	}
	if taskErr := normalizeAndValidateRequest(&request, genericImages, profile); taskErr != nil {
		return taskErr
	}
	sanitizeRequestForFormat(&request, profile.format)
	billingBody, err := common.Marshal(request)
	if err != nil {
		return service.TaskErrorWrapperLocal(errors.Wrap(err, "marshal normalized TMLab Seedance billing request failed"), "billing_request_failed", http.StatusInternalServerError)
	}
	info.BillingRequestInput = &billingexpr.RequestInput{Body: billingBody}

	c.Set(taskRequestContextKey, request)
	return nil
}

func invalidRequest(err error, code string) *taskdto.TaskError {
	return service.TaskErrorWrapperLocal(err, code, http.StatusBadRequest)
}

func intPtr(value int) *int {
	return &value
}

func fillMissingRequestFields(target, fallback *submitRequest) {
	if target.Duration == nil {
		target.Duration = fallback.Duration
	}
	if target.DurationSec == nil {
		target.DurationSec = fallback.DurationSec
	}
	if target.ModeType == "" {
		target.ModeType = fallback.ModeType
	}
	if target.Ratio == "" {
		target.Ratio = fallback.Ratio
	}
	if target.AspectRatio == "" {
		target.AspectRatio = fallback.AspectRatio
	}
	if target.Resolution == "" {
		target.Resolution = fallback.Resolution
	}
	if target.EnableSound == "" {
		target.EnableSound = fallback.EnableSound
	}
	if target.EnableFaceMask == nil {
		target.EnableFaceMask = fallback.EnableFaceMask
	}
	if target.ReferenceURL == "" {
		target.ReferenceURL = fallback.ReferenceURL
	}
	if target.FirstImage == "" {
		target.FirstImage = fallback.FirstImage
	}
	if target.LastImage == "" {
		target.LastImage = fallback.LastImage
	}
	if len(target.Images) == 0 {
		target.Images = fallback.Images
	}
	if len(target.ImageURLs) == 0 {
		target.ImageURLs = fallback.ImageURLs
	}
	if len(target.AudioURLs) == 0 {
		target.AudioURLs = fallback.AudioURLs
	}
	if len(target.VideoURLs) == 0 {
		target.VideoURLs = fallback.VideoURLs
	}
	if len(target.MixedList) == 0 {
		target.MixedList = fallback.MixedList
	}
	if len(target.ReferenceURLs) == 0 {
		target.ReferenceURLs = fallback.ReferenceURLs
	}
	if len(target.InputImages) == 0 {
		target.InputImages = fallback.InputImages
	}
	if len(target.InputAudios) == 0 {
		target.InputAudios = fallback.InputAudios
	}
	if len(target.ReferenceImages) == 0 {
		target.ReferenceImages = fallback.ReferenceImages
	}
	if len(target.ReferenceVideos) == 0 {
		target.ReferenceVideos = fallback.ReferenceVideos
	}
	if len(target.ReferenceAudios) == 0 {
		target.ReferenceAudios = fallback.ReferenceAudios
	}
}

func normalizeAndValidateRequest(request *submitRequest, genericImages []string, profile modelProfile) *taskdto.TaskError {
	promptLength := utf8.RuneCountInString(request.Prompt)
	if profile.promptMin > 0 && promptLength < profile.promptMin {
		return invalidRequest(fmt.Errorf("prompt must contain at least %d characters", profile.promptMin), "invalid_prompt")
	}
	if profile.promptMax > 0 && promptLength > profile.promptMax {
		return invalidRequest(fmt.Errorf("prompt must contain at most %d characters", profile.promptMax), "invalid_prompt")
	}

	switch profile.format {
	case requestFormatStable:
		if len(request.VideoURLs) > 0 || len(request.ReferenceVideos) > 0 {
			return invalidRequest(fmt.Errorf("this model does not support video references"), "unsupported_video_reference")
		}
		if len(request.ImageURLs) == 0 {
			request.ImageURLs = genericImages
		}
		request.Images = nil
		if request.ModeType == "" {
			switch {
			case len(request.MixedList) > 0:
				request.ModeType = "mixed2video"
			case len(request.ImageURLs) > 0:
				request.ModeType = "image2video"
			default:
				request.ModeType = "text2video"
			}
		}
		if request.EnableSound == "" {
			request.EnableSound = "off"
		}
		if request.EnableSound != "on" && request.EnableSound != "off" {
			return invalidRequest(fmt.Errorf("enable_sound must be on or off"), "invalid_enable_sound")
		}
		if len(request.ImageURLs) > 9 {
			return invalidRequest(fmt.Errorf("image_urls supports at most 9 items"), "invalid_image_urls")
		}
		if len(request.AudioURLs) > 3 {
			return invalidRequest(fmt.Errorf("audio_urls supports at most 3 items"), "invalid_audio_urls")
		}
		if len(request.MixedList) > 15 {
			return invalidRequest(fmt.Errorf("mixed_list supports at most 15 items"), "invalid_mixed_list")
		}
		switch request.ModeType {
		case "text2video":
			if len(request.ImageURLs) > 0 || len(request.AudioURLs) > 0 || len(request.MixedList) > 0 {
				return invalidRequest(fmt.Errorf("text2video does not accept reference media"), "invalid_mode_type")
			}
		case "image2video":
			if len(request.ImageURLs) == 0 || len(request.MixedList) > 0 {
				return invalidRequest(fmt.Errorf("image2video requires image_urls and does not accept mixed_list"), "invalid_mode_type")
			}
		case "mixed2video":
			if len(request.MixedList) == 0 || len(request.ImageURLs) > 0 {
				return invalidRequest(fmt.Errorf("mixed2video requires mixed_list and does not accept image_urls"), "invalid_mode_type")
			}
		default:
			return invalidRequest(fmt.Errorf("unsupported mode_type: %s", request.ModeType), "invalid_mode_type")
		}
		for _, item := range request.MixedList {
			if item.Type != "image" && item.Type != "audio" {
				return invalidRequest(fmt.Errorf("mixed_list type must be image or audio"), "invalid_mixed_list")
			}
			if err := validatePublicURL(item.URL, false); err != nil {
				return invalidRequest(err, "invalid_mixed_list")
			}
		}
		if err := validateURLList(request.ImageURLs, false); err != nil {
			return invalidRequest(err, "invalid_image_urls")
		}
		if err := validateURLList(request.AudioURLs, false); err != nil {
			return invalidRequest(err, "invalid_audio_urls")
		}

	case requestFormatV2:
		if len(request.VideoURLs) > 0 || len(request.ReferenceVideos) > 0 {
			return invalidRequest(fmt.Errorf("this model does not support video references"), "unsupported_video_reference")
		}
		if len(request.Images) == 0 {
			request.Images = genericImages
		}
		if len(request.Images) > 9 {
			return invalidRequest(fmt.Errorf("images supports at most 9 items"), "invalid_images")
		}
		if len(request.AudioURLs) > 3 {
			return invalidRequest(fmt.Errorf("audio_urls supports at most 3 items"), "invalid_audio_urls")
		}
		if err := validateURLList(request.Images, false); err != nil {
			return invalidRequest(err, "invalid_images")
		}
		if err := validateURLList(request.AudioURLs, false); err != nil {
			return invalidRequest(err, "invalid_audio_urls")
		}

	case requestFormatPro720P:
		if len(request.ReferenceURLs) == 0 && request.ReferenceURL == "" && len(genericImages) > 0 {
			request.ReferenceURLs = genericImages
		}
		request.Images = nil
		if request.ReferenceURL != "" && len(request.ReferenceURLs) > 0 {
			return invalidRequest(fmt.Errorf("reference_url and reference_urls cannot be used together"), "invalid_references")
		}
		if len(request.ReferenceURLs) > 10 {
			return invalidRequest(fmt.Errorf("reference_urls supports at most 10 items"), "invalid_references")
		}
		if request.ReferenceURL != "" {
			if err := validatePublicURL(request.ReferenceURL, true); err != nil {
				return invalidRequest(err, "invalid_reference_url")
			}
		}
		if err := validateURLList(request.ReferenceURLs, true); err != nil {
			return invalidRequest(err, "invalid_reference_urls")
		}

	case requestFormat431:
		if len(request.ReferenceImages) == 0 {
			request.ReferenceImages = genericImages
		}
		request.Images = nil
		if len(request.ReferenceImages) > 4 {
			return invalidRequest(fmt.Errorf("referenceImages supports at most 4 items"), "invalid_reference_images")
		}
		if len(request.ReferenceVideos) > 3 {
			return invalidRequest(fmt.Errorf("referenceVideos supports at most 3 items"), "invalid_reference_videos")
		}
		if len(request.ReferenceAudios) > 1 {
			return invalidRequest(fmt.Errorf("referenceAudios supports at most 1 item"), "invalid_reference_audios")
		}
		hasFrameMode := request.FirstImage != "" || request.LastImage != ""
		hasReferenceMode := len(request.ReferenceImages) > 0 || len(request.ReferenceVideos) > 0 || len(request.ReferenceAudios) > 0
		if hasFrameMode && hasReferenceMode {
			return invalidRequest(fmt.Errorf("first/last frame mode cannot be combined with reference media"), "invalid_references")
		}
		for _, rawURL := range []string{request.FirstImage, request.LastImage} {
			if rawURL != "" {
				if err := validatePublicURL(rawURL, false); err != nil {
					return invalidRequest(err, "invalid_frame_url")
				}
			}
		}
		for _, item := range []struct {
			values []string
			code   string
		}{
			{request.ReferenceImages, "invalid_reference_images"},
			{request.ReferenceVideos, "invalid_reference_videos"},
			{request.ReferenceAudios, "invalid_reference_audios"},
		} {
			if err := validateURLList(item.values, false); err != nil {
				return invalidRequest(err, item.code)
			}
		}

	case requestFormat25:
		if len(request.VideoURLs) > 0 || len(request.ReferenceVideos) > 0 {
			return invalidRequest(fmt.Errorf("this model does not support video references"), "unsupported_video_reference")
		}
		if len(request.InputImages) == 0 {
			request.InputImages = genericImages
		}
		request.Images = nil
		if len(request.InputImages) > 9 {
			return invalidRequest(fmt.Errorf("input_images supports at most 9 items"), "invalid_input_images")
		}
		if len(request.InputAudios) > 3 {
			return invalidRequest(fmt.Errorf("input_audios supports at most 3 items"), "invalid_input_audios")
		}
		if err := validateURLList(request.InputImages, false); err != nil {
			return invalidRequest(err, "invalid_input_images")
		}
		if err := validateURLList(request.InputAudios, false); err != nil {
			return invalidRequest(err, "invalid_input_audios")
		}
	}

	if profile.format == requestFormatPro720P || profile.format == requestFormat25 {
		if request.DurationSec == nil && request.Duration != nil {
			request.DurationSec = request.Duration
		}
		request.Duration = nil
	} else if request.Duration == nil && request.DurationSec != nil {
		request.Duration = request.DurationSec
		request.DurationSec = nil
	}

	duration := request.Duration
	if profile.format == requestFormatPro720P || profile.format == requestFormat25 {
		duration = request.DurationSec
	}
	if duration == nil {
		return invalidRequest(fmt.Errorf("duration is required"), "missing_duration")
	}
	if *duration <= 0 || *duration > relaycommon.MaxTaskDurationSeconds {
		return invalidRequest(fmt.Errorf("duration must be between 1 and %d", relaycommon.MaxTaskDurationSeconds), "invalid_duration")
	}
	if len(profile.allowedDurations) > 0 {
		if _, ok := profile.allowedDurations[*duration]; !ok {
			return invalidRequest(fmt.Errorf("unsupported duration: %d", *duration), "invalid_duration")
		}
	} else if *duration < profile.durationMin || (profile.durationMax > 0 && *duration > profile.durationMax) {
		return invalidRequest(fmt.Errorf("duration must be between %d and %d seconds", profile.durationMin, profile.durationMax), "invalid_duration")
	}

	if profile.format == requestFormatPro720P {
		if request.AspectRatio == "" {
			request.AspectRatio = profile.defaultRatio
		}
		request.Ratio = ""
		if _, ok := profile.allowedRatios[request.AspectRatio]; !ok {
			return invalidRequest(fmt.Errorf("unsupported aspect_ratio: %s", request.AspectRatio), "invalid_aspect_ratio")
		}
	} else {
		if request.Ratio == "" {
			request.Ratio = profile.defaultRatio
		}
		request.AspectRatio = ""
		if _, ok := profile.allowedRatios[request.Ratio]; !ok {
			return invalidRequest(fmt.Errorf("unsupported ratio: %s", request.Ratio), "invalid_ratio")
		}
	}
	if request.Resolution == "" {
		request.Resolution = profile.defaultResolution
	}
	if _, ok := profile.resolutionRatios[request.Resolution]; !ok {
		return invalidRequest(fmt.Errorf("unsupported resolution: %s", request.Resolution), "invalid_resolution")
	}

	return nil
}

func validateURLList(values []string, httpsOnly bool) error {
	for _, value := range values {
		if err := validatePublicURL(value, httpsOnly); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicURL(rawURL string, httpsOnly bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid reference URL: %s", rawURL)
	}
	if httpsOnly && parsed.Scheme != "https" {
		return fmt.Errorf("reference URL must use HTTPS: %s", rawURL)
	}
	if !httpsOnly && parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("reference URL must use HTTP or HTTPS: %s", rawURL)
	}
	return nil
}

func sanitizeRequestForFormat(request *submitRequest, format requestFormat) {
	switch format {
	case requestFormatStable:
		request.Images = nil
		request.VideoURLs = nil
		request.DurationSec = nil
		request.AspectRatio = ""
		request.ReferenceURL = ""
		request.ReferenceURLs = nil
		request.EnableFaceMask = nil
		request.InputImages = nil
		request.InputAudios = nil
		request.FirstImage = ""
		request.LastImage = ""
		request.ReferenceImages = nil
		request.ReferenceVideos = nil
		request.ReferenceAudios = nil
	case requestFormatV2:
		request.ModeType = ""
		request.EnableSound = ""
		request.ImageURLs = nil
		request.VideoURLs = nil
		request.MixedList = nil
		request.DurationSec = nil
		request.AspectRatio = ""
		request.ReferenceURL = ""
		request.ReferenceURLs = nil
		request.EnableFaceMask = nil
		request.InputImages = nil
		request.InputAudios = nil
		request.FirstImage = ""
		request.LastImage = ""
		request.ReferenceImages = nil
		request.ReferenceVideos = nil
		request.ReferenceAudios = nil
	case requestFormatPro720P:
		request.ModeType = ""
		request.Duration = nil
		request.Ratio = ""
		request.EnableSound = ""
		request.ImageURLs = nil
		request.AudioURLs = nil
		request.VideoURLs = nil
		request.MixedList = nil
		request.Images = nil
		request.InputImages = nil
		request.InputAudios = nil
		request.FirstImage = ""
		request.LastImage = ""
		request.ReferenceImages = nil
		request.ReferenceVideos = nil
		request.ReferenceAudios = nil
	case requestFormat431:
		request.ModeType = ""
		request.EnableSound = ""
		request.ImageURLs = nil
		request.AudioURLs = nil
		request.VideoURLs = nil
		request.MixedList = nil
		request.Images = nil
		request.DurationSec = nil
		request.AspectRatio = ""
		request.ReferenceURL = ""
		request.ReferenceURLs = nil
		request.EnableFaceMask = nil
		request.InputImages = nil
		request.InputAudios = nil
	case requestFormat25:
		request.ModeType = ""
		request.Duration = nil
		request.EnableSound = ""
		request.ImageURLs = nil
		request.AudioURLs = nil
		request.VideoURLs = nil
		request.MixedList = nil
		request.Images = nil
		request.AspectRatio = ""
		request.ReferenceURL = ""
		request.ReferenceURLs = nil
		request.EnableFaceMask = nil
		request.FirstImage = ""
		request.LastImage = ""
		request.ReferenceImages = nil
		request.ReferenceVideos = nil
		request.ReferenceAudios = nil
	}
}

func getSubmitRequest(c *gin.Context) (submitRequest, error) {
	value, ok := c.Get(taskRequestContextKey)
	if !ok {
		return submitRequest{}, fmt.Errorf("TMLab Seedance request not found in context")
	}
	request, ok := value.(submitRequest)
	if !ok {
		return submitRequest{}, fmt.Errorf("invalid TMLab Seedance request type")
	}
	return request, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	request, err := getSubmitRequest(c)
	if err != nil {
		return nil
	}
	profile, ok := modelProfiles[request.Model]
	if !ok || profile.fixedPrice {
		return nil
	}
	duration := request.Duration
	if profile.format == requestFormatPro720P || profile.format == requestFormat25 {
		duration = request.DurationSec
	}
	if duration == nil || *duration <= 0 {
		return nil
	}
	ratios := map[string]float64{"seconds": float64(*duration)}
	if resolutionRatio := profile.resolutionRatios[request.Resolution]; resolutionRatio != 1 {
		ratios["resolution"] = resolutionRatio
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/v1/tasks", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	request, err := getSubmitRequest(c)
	if err != nil {
		return nil, err
	}
	request.Model = info.UpstreamModelName
	if request.Model == "" {
		request.Model = info.OriginModelName
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, errors.Wrap(err, "marshal TMLab Seedance request failed")
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstreamResponse taskResponse
	if err := common.Unmarshal(responseBody, &upstreamResponse); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	taskID := upstreamResponse.TaskID
	if taskID == "" {
		taskID = upstreamResponse.ID
	}
	if taskID == "" {
		message := responseErrorMessage(upstreamResponse)
		if message == "" {
			message = "task_id is empty"
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", message), "invalid_response", http.StatusBadGateway)
	}

	var downstreamResponse []byte
	if strings.HasPrefix(c.Request.URL.Path, "/v1/tasks") {
		var nativeResponse map[string]any
		if err := common.Unmarshal(responseBody, &nativeResponse); err != nil {
			return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "unmarshal native TMLab response failed"), "invalid_response", http.StatusBadGateway)
		}
		replaceTaskIDs(nativeResponse, info.PublicTaskID, true)
		downstreamResponse, err = common.Marshal(nativeResponse)
	} else {
		video := relaydto.NewOpenAIVideo()
		video.ID = info.PublicTaskID
		video.TaskID = info.PublicTaskID
		video.CreatedAt = time.Now().Unix()
		video.Model = info.OriginModelName
		downstreamResponse, err = common.Marshal(video)
	}
	if err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "marshal downstream task response failed"), "marshal_response_failed", http.StatusInternalServerError)
	}
	info.PendingResponse = downstreamResponse
	info.PendingResponseStatusCode = http.StatusOK
	return taskID, a.SanitizeTaskResponse(responseBody, info.PublicTaskID), nil
}

func (a *TaskAdaptor) SanitizeTaskResponse(responseBody []byte, publicTaskID string) []byte {
	var response map[string]any
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return responseBody
	}
	replaceTaskIDs(response, publicTaskID, false)
	sanitized, err := common.Marshal(response)
	if err != nil {
		return responseBody
	}
	return sanitized
}

func replaceTaskIDs(response map[string]any, publicTaskID string, ensureTaskID bool) {
	if ensureTaskID {
		response["task_id"] = publicTaskID
	} else if _, hasTaskID := response["task_id"]; hasTaskID {
		response["task_id"] = publicTaskID
	}
	if _, hasID := response["id"]; hasID {
		response["id"] = publicTaskID
	}
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	requestURL := strings.TrimRight(baseURL, "/") + "/v1/tasks/" + url.PathEscape(taskID)
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(responseBody []byte) (*relaycommon.TaskInfo, error) {
	var upstreamResponse taskResponse
	if err := common.Unmarshal(responseBody, &upstreamResponse); err != nil {
		return nil, errors.Wrap(err, "unmarshal TMLab Seedance task result failed")
	}

	result := &relaycommon.TaskInfo{TaskID: upstreamResponse.TaskID}
	if result.TaskID == "" {
		result.TaskID = upstreamResponse.ID
	}
	status := strings.ToLower(strings.TrimSpace(upstreamResponse.Status))
	if status == "" {
		return result, nil
	}
	switch status {
	case "queued", "pending":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "in_progress", "processing", "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(upstreamResponse.Progress, taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded":
		result.Url = responseVideoURL(upstreamResponse)
		result.RemoteUrl = upstreamResponse.RemoteURL
		if result.Url == "" {
			result.Status = model.TaskStatusInProgress
			result.Progress = progressString(upstreamResponse.Progress, taskcommon.ProgressInProgress)
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
	case "failed", "failure":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseErrorMessage(upstreamResponse)
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(upstreamResponse.Progress, taskcommon.ProgressInProgress)
	}
	return result, nil
}

func progressString(progress any, fallback string) string {
	switch value := progress.(type) {
	case float64:
		if value >= 0 && value <= 100 {
			return strconv.Itoa(int(value)) + "%"
		}
	case string:
		trimmed := strings.TrimSpace(strings.TrimSuffix(value, "%"))
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil && parsed >= 0 && parsed <= 100 {
			return strconv.Itoa(int(parsed)) + "%"
		}
	}
	return fallback
}

func responseVideoURL(response taskResponse) string {
	for _, candidate := range []string{response.Metadata.URL, response.ResultURL, response.VideoURL, response.RemoteURL} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func responseErrorMessage(response taskResponse) string {
	if response.FailureReason != "" {
		return response.FailureReason
	}
	switch value := response.Error.(type) {
	case string:
		return value
	case map[string]any:
		if message, ok := value["message"].(string); ok {
			return message
		}
	}
	return ""
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var upstreamResponse taskResponse
	if len(originTask.Data) > 0 {
		if err := common.Unmarshal(originTask.Data, &upstreamResponse); err != nil {
			return nil, errors.Wrap(err, "unmarshal TMLab Seedance task data failed")
		}
	}

	video := relaydto.NewOpenAIVideo()
	video.ID = originTask.TaskID
	video.TaskID = originTask.TaskID
	video.Status = originTask.Status.ToVideoStatus()
	video.SetProgressStr(originTask.Progress)
	if resultURL := responseVideoURL(upstreamResponse); resultURL != "" {
		video.SetMetadata("url", resultURL)
	}
	video.CreatedAt = originTask.CreatedAt
	if originTask.Status == model.TaskStatusSuccess || originTask.Status == model.TaskStatusFailure {
		video.CompletedAt = originTask.UpdatedAt
	}
	video.Model = originTask.Properties.OriginModelName
	if originTask.Status == model.TaskStatusFailure {
		message := responseErrorMessage(upstreamResponse)
		if message == "" {
			message = originTask.FailReason
		}
		video.Error = &relaydto.OpenAIVideoError{
			Message: message,
			Code:    "generation_failed",
		}
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) ConvertToNativeTask(originTask *model.Task) ([]byte, error) {
	response := make(map[string]any)
	if len(originTask.Data) > 0 {
		if err := common.Unmarshal(originTask.Data, &response); err != nil {
			return nil, errors.Wrap(err, "unmarshal TMLab Seedance task data failed")
		}
	}

	response["task_id"] = originTask.TaskID
	if _, hasID := response["id"]; hasID {
		response["id"] = originTask.TaskID
	}
	storedStatus, hasStatus := response["status"]
	if !hasStatus {
		response["status"] = nativeTaskStatus(originTask.Status)
	}
	if originTask.Status == model.TaskStatusFailure {
		statusText, _ := storedStatus.(string)
		normalizedStatus := strings.ToLower(strings.TrimSpace(statusText))
		if normalizedStatus != "failed" && normalizedStatus != "failure" {
			// Local failures (for example a deleted channel) may still carry the
			// queued submit payload. Do not publish that stale state.
			response["status"] = "failed"
		}
		if originTask.FailReason != "" {
			response["failure_reason"] = originTask.FailReason
		}
	}
	if _, hasProgress := response["progress"]; !hasProgress && originTask.Progress != "" {
		response["progress"] = originTask.Progress
	}
	if resultURL := originTask.PrivateData.ResultURL; resultURL != "" {
		metadata, _ := response["metadata"].(map[string]any)
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["url"] = resultURL
		response["metadata"] = metadata
	}
	return common.Marshal(response)
}

func nativeTaskStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSubmitted, model.TaskStatusQueued:
		return "queued"
	case model.TaskStatusInProgress:
		return "processing"
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	default:
		return "pending"
	}
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}
