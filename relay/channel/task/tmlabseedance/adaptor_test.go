package tmlabseedance

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTaskContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func validateRequest(t *testing.T, body string) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	context, _ := newTaskContext(t, body)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
	request, err := getSubmitRequest(context)
	require.NoError(t, err)
	info.OriginModelName = request.Model
	info.UpstreamModelName = request.Model
	info.PublicTaskID = "task_public"
	return adaptor, context, info
}

func decodeBuiltRequest(t *testing.T, adaptor *TaskAdaptor, context *gin.Context, info *relaycommon.RelayInfo) submitRequest {
	t.Helper()
	body, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var request submitRequest
	require.NoError(t, common.Unmarshal(data, &request))
	return request
}

func TestModelListContainsAllTMLabSeedanceModels(t *testing.T) {
	assert.Equal(t, []string{
		"[V2]seedance-2.0",
		"seedance-2.0-mini",
		"seedance-2.0-fast",
		"seedance-2.0-pro",
		"seedance-2.0-pro-720p",
		"seedance-2.0-fast(431)",
		"seedance-2.0-pro(431)",
		"seedance-2.5",
	}, (&TaskAdaptor{}).GetModelList())
}

func TestPollingIntervalFollowsTMLabModelGuidance(t *testing.T) {
	adaptor := &TaskAdaptor{}
	assert.Equal(t, 30*time.Second, adaptor.PollingInterval(&model.Task{Properties: model.Properties{UpstreamModelName: ModelSeedanceFast431}}))
	assert.Equal(t, 5*time.Second, adaptor.PollingInterval(&model.Task{Properties: model.Properties{UpstreamModelName: ModelSeedanceV2}}))
	assert.Equal(t, 5*time.Second, adaptor.PollingInterval(&model.Task{Properties: model.Properties{UpstreamModelName: ModelSeedancePro720P}}))
	assert.Equal(t, 10*time.Second, adaptor.PollingInterval(&model.Task{Properties: model.Properties{UpstreamModelName: ModelSeedanceFast}}))
}

func TestNativeTaskFallbackStatusesStayWithinDocumentedContract(t *testing.T) {
	assert.Equal(t, "queued", nativeTaskStatus(model.TaskStatusUnknown, ModelSeedanceV2))
	assert.Equal(t, "queued", nativeTaskStatus(model.TaskStatusQueued, ModelSeedanceV2))
	assert.Equal(t, "in_progress", nativeTaskStatus(model.TaskStatusInProgress, ModelSeedanceV2))
	assert.Equal(t, "completed", nativeTaskStatus(model.TaskStatusSuccess, ModelSeedanceV2))
	assert.Equal(t, "failed", nativeTaskStatus(model.TaskStatusFailure, ModelSeedanceV2))

	assert.Equal(t, "QUEUED", nativeTaskStatus(model.TaskStatusUnknown, ModelSeedanceFast431))
	assert.Equal(t, "QUEUED", nativeTaskStatus(model.TaskStatusQueued, ModelSeedanceFast431))
	assert.Equal(t, "IN_PROGRESS", nativeTaskStatus(model.TaskStatusInProgress, ModelSeedanceFast431))
	assert.Equal(t, "SUCCESS", nativeTaskStatus(model.TaskStatusSuccess, ModelSeedanceFast431))
	assert.Equal(t, "FAILURE", nativeTaskStatus(model.TaskStatusFailure, ModelSeedanceFast431))
}

func TestValidateAndBuildStableRequest(t *testing.T) {
	body := `{
		"model":"seedance-2.0-fast",
		"prompt":"cinematic forest",
		"duration":8,
		"resolution":"720p",
		"images":["https://example.com/frame.png"],
		"audio_urls":["https://example.com/music.mp3"]
	}`
	adaptor, context, info := validateRequest(t, body)

	ratios := adaptor.EstimateBilling(context, info)
	assert.Equal(t, map[string]float64{"seconds": 8, "resolution": 2}, ratios)

	request := decodeBuiltRequest(t, adaptor, context, info)
	assert.Equal(t, "image2video", request.ModeType)
	assert.Equal(t, "off", request.EnableSound)
	assert.Equal(t, "adaptive", request.Ratio)
	assert.Equal(t, []string{"https://example.com/frame.png"}, request.ImageURLs)
	assert.Empty(t, request.Images)
	require.NotNil(t, request.Duration)
	assert.Equal(t, 8, *request.Duration)
}

func TestBillingInputUsesNormalizedTMLabRequest(t *testing.T) {
	_, _, info := validateRequest(t, `{
		"model":"seedance-2.0-fast",
		"prompt":"cinematic forest",
		"seconds":"4",
		"size":"1280x720"
	}`)

	require.NotNil(t, info.BillingRequestInput)
	var billingRequest submitRequest
	require.NoError(t, common.Unmarshal(info.BillingRequestInput.Body, &billingRequest))
	require.NotNil(t, billingRequest.Duration)
	assert.Equal(t, 4, *billingRequest.Duration)
	assert.Equal(t, "720p", billingRequest.Resolution)
	assert.Equal(t, "16:9", billingRequest.Ratio)
}

func TestExtractUsageFactsUsesNormalizedTMLabRequest(t *testing.T) {
	adaptor, context, info := validateRequest(t, `{
		"model":"seedance-2.0-fast",
		"prompt":"cinematic forest",
		"duration":8,
		"resolution":"720p",
		"images":["https://example.com/frame.png"]
	}`)

	assert.Equal(t, map[string]any{
		"duration":         float64(8),
		"seconds":          float64(8),
		"resolution":       "720p",
		"resolution_ratio": float64(2),
		"ratio":            "adaptive",
		"mode_type":        "image2video",
	}, adaptor.ExtractUsageFacts(context, info))
}

func TestValidateOpenAIVideoFieldsForTMLab(t *testing.T) {
	body := `{
		"model":"[V2]seedance-2.0",
		"prompt":"cinematic forest",
		"seconds":"6",
		"size":"1920x1080",
		"input_reference":"https://example.com/frame.png"
	}`
	adaptor, context, info := validateRequest(t, body)
	request := decodeBuiltRequest(t, adaptor, context, info)

	require.NotNil(t, request.Duration)
	assert.Equal(t, 6, *request.Duration)
	assert.Equal(t, "16:9", request.Ratio)
	assert.Equal(t, "1080P", request.Resolution)
	assert.Equal(t, []string{"https://example.com/frame.png"}, request.Images)
}

func TestMappedModelUsesUpstreamDialect(t *testing.T) {
	context, _ := newTaskContext(t, `{
		"model":"seedance-alias",
		"prompt":"cinematic forest",
		"duration":8,
		"resolution":"720p"
	}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-alias",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: ModelSeedanceFast,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
	request := decodeBuiltRequest(t, adaptor, context, info)

	assert.Equal(t, ModelSeedanceFast, request.Model)
	assert.Equal(t, "text2video", request.ModeType)
	assert.Equal(t, "off", request.EnableSound)
}

func TestValidateAndBuildEachTMLabDialect(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		assertBody func(t *testing.T, request submitRequest, ratios map[string]float64)
	}{
		{
			name: "v2",
			body: `{"model":"[V2]seedance-2.0","prompt":"product video","duration":6,"resolution":"1080P","images":["https://example.com/a.png"]}`,
			assertBody: func(t *testing.T, request submitRequest, ratios map[string]float64) {
				assert.InDelta(t, 1.0/0.75, ratios["resolution"], 0.000001)
				assert.Equal(t, float64(6), ratios["seconds"])
				assert.Equal(t, "16:9", request.Ratio)
				assert.Equal(t, []string{"https://example.com/a.png"}, request.Images)
			},
		},
		{
			name: "pro 720p",
			body: `{"model":"seedance-2.0-pro-720p","prompt":"vertical cinematic scene","duration_sec":10,"reference_url":"https://example.com/a.png"}`,
			assertBody: func(t *testing.T, request submitRequest, ratios map[string]float64) {
				assert.Equal(t, map[string]float64{"seconds": 10}, ratios)
				assert.Equal(t, "9:16", request.AspectRatio)
				assert.Equal(t, "720p", request.Resolution)
				require.NotNil(t, request.DurationSec)
				assert.Nil(t, request.Duration)
			},
		},
		{
			name: "fixed pro 431",
			body: `{"model":"seedance-2.0-pro(431)","prompt":"cinematic scene","duration":8,"ratio":"1:1","referenceImages":["https://example.com/a.png"]}`,
			assertBody: func(t *testing.T, request submitRequest, ratios map[string]float64) {
				assert.Nil(t, ratios)
				assert.Equal(t, []string{"https://example.com/a.png"}, request.ReferenceImages)
				assert.Equal(t, "720p", request.Resolution)
			},
		},
		{
			name: "seedance 2.5",
			body: `{"model":"seedance-2.5","prompt":"cinematic sea","duration_sec":5,"ratio":"4:3","resolution":"720p","input_audios":["https://example.com/a.mp3"]}`,
			assertBody: func(t *testing.T, request submitRequest, ratios map[string]float64) {
				assert.Equal(t, float64(5), ratios["seconds"])
				assert.Equal(t, 1.7/0.8, ratios["resolution"])
				assert.Equal(t, []string{"https://example.com/a.mp3"}, request.InputAudios)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor, context, info := validateRequest(t, test.body)
			request := decodeBuiltRequest(t, adaptor, context, info)
			test.assertBody(t, request, adaptor.EstimateBilling(context, info))
		})
	}
}

func TestMetadataCompatibilityCannotOverrideModel(t *testing.T) {
	body := `{
		"model":"seedance-2.0-mini",
		"prompt":"cinematic sea",
		"duration":5,
		"metadata":{"model":"seedance-2.0-pro(431)","resolution":"480p","ratio":"16:9","enable_sound":"off"}
	}`
	adaptor, context, info := validateRequest(t, body)
	request := decodeBuiltRequest(t, adaptor, context, info)

	assert.Equal(t, ModelSeedanceMini, request.Model)
	assert.Equal(t, "480p", request.Resolution)
	assert.Equal(t, "off", request.EnableSound)
	assert.Equal(t, map[string]float64{"seconds": 5}, adaptor.EstimateBilling(context, info))
}

func TestRequestBodyDropsFieldsFromOtherTMLabDialects(t *testing.T) {
	body := `{
		"model":"seedance-2.5",
		"prompt":"cinematic sea",
		"duration_sec":5,
		"ratio":"16:9",
		"resolution":"480p",
		"enable_face_mask":true
	}`
	adaptor, context, info := validateRequest(t, body)
	request := decodeBuiltRequest(t, adaptor, context, info)

	assert.Nil(t, request.EnableFaceMask)
	assert.Equal(t, "480p", request.Resolution)
}

func TestRequestValidationRejectsBillingAndProtocolBypasses(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "duration above global bound",
			body: `{"model":"seedance-2.5","prompt":"video","duration_sec":3601,"ratio":"16:9","resolution":"480p"}`,
			code: "invalid_duration",
		},
		{
			name: "fast 431 duration outside allowlist",
			body: `{"model":"seedance-2.0-fast(431)","prompt":"video","duration":14}`,
			code: "invalid_duration",
		},
		{
			name: "too many images",
			body: `{"model":"seedance-2.0-fast(431)","prompt":"video","duration":10,"referenceImages":["https://example.com/1","https://example.com/2","https://example.com/3","https://example.com/4","https://example.com/5"]}`,
			code: "invalid_reference_images",
		},
		{
			name: "frame and reference modes conflict",
			body: `{"model":"seedance-2.0-pro(431)","prompt":"video","duration":8,"first_image":"https://example.com/first.png","referenceVideos":["https://example.com/a.mp4"]}`,
			code: "invalid_references",
		},
		{
			name: "pro 720p requires https reference",
			body: `{"model":"seedance-2.0-pro-720p","prompt":"vertical cinematic scene","duration_sec":5,"reference_url":"http://example.com/a.png"}`,
			code: "invalid_reference_url",
		},
		{
			name: "stable mixed mode rejects video",
			body: `{"model":"seedance-2.0-fast","prompt":"video","duration":5,"mode_type":"mixed2video","mixed_list":[{"url":"https://example.com/a.mp4","type":"video"}]}`,
			code: "invalid_mixed_list",
		},
		{
			name: "seedance 2.5 rejects video reference fields",
			body: `{"model":"seedance-2.5","prompt":"video","duration_sec":5,"ratio":"16:9","resolution":"480p","referenceVideos":["https://example.com/a.mp4"]}`,
			code: "unsupported_video_reference",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := newTaskContext(t, test.body)
			adaptor := &TaskAdaptor{}
			taskErr := adaptor.ValidateRequestAndSetAction(context, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
			require.NotNil(t, taskErr)
			assert.Equal(t, test.code, taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		})
	}
}

func TestMultipartInputReferenceFileIsRejectedWithActionableError(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", ModelSeedanceFast))
	require.NoError(t, writer.WriteField("prompt", "cinematic forest"))
	require.NoError(t, writer.WriteField("seconds", "5"))
	file, err := writer.CreateFormFile("input_reference", "frame.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	context, _ := newTaskContext(t, "")
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage, err := common.GetBodyStorage(context)
	require.NoError(t, err)
	context.Request.Body = io.NopCloser(storage)
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, "unsupported_input_reference_file", taskErr.Code)
	assert.Contains(t, taskErr.Message, "public URL")
}

func TestParseTaskResultSupportsBothStatusFamilies(t *testing.T) {
	adaptor := &TaskAdaptor{}
	tests := []struct {
		name     string
		body     string
		status   model.TaskStatus
		progress string
		url      string
		reason   string
	}{
		{
			name:     "lowercase completed metadata URL",
			body:     `{"id":"upstream-1","status":"completed","progress":100,"metadata":{"url":"https://example.com/result.mp4"}}`,
			status:   model.TaskStatusSuccess,
			progress: "100%",
			url:      "https://example.com/result.mp4",
		},
		{
			name:     "uppercase success result URL",
			body:     `{"task_id":"upstream-2","status":"SUCCESS","result_url":"https://example.com/result.mp4"}`,
			status:   model.TaskStatusSuccess,
			progress: "100%",
			url:      "https://example.com/result.mp4",
		},
		{
			name:     "in progress preserves numeric progress",
			body:     `{"task_id":"upstream-3","status":"IN_PROGRESS","progress":"55"}`,
			status:   model.TaskStatusInProgress,
			progress: "55%",
		},
		{
			name:     "failure reason",
			body:     `{"task_id":"upstream-4","status":"FAILURE","failure_reason":"generation rejected"}`,
			status:   model.TaskStatusFailure,
			progress: "100%",
			reason:   "generation rejected",
		},
		{
			name:     "nested error message",
			body:     `{"id":"upstream-5","status":"failed","error":{"message":"invalid media"}}`,
			status:   model.TaskStatusFailure,
			progress: "100%",
			reason:   "invalid media",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := adaptor.ParseTaskResult([]byte(test.body))
			require.NoError(t, err)
			assert.EqualValues(t, test.status, result.Status)
			assert.Equal(t, test.progress, result.Progress)
			assert.Equal(t, test.url, result.Url)
			assert.Equal(t, test.reason, result.Reason)
		})
	}
}

func TestParseTaskResultLeavesStructuredErrorsForPollingClassifier(t *testing.T) {
	adaptor := &TaskAdaptor{}

	result, err := adaptor.ParseTaskResult([]byte(`{"error":{"message":"invalid API key"}}`))
	require.NoError(t, err)
	assert.Empty(t, result.Status)
}

func TestParseTaskResultWaitsWhenSuccessHasNoVideoURL(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"task_id":"upstream","status":"completed","progress":100}`))
	require.NoError(t, err)
	assert.EqualValues(t, model.TaskStatusInProgress, result.Status)
	assert.Empty(t, result.Url)
}

func TestParseResponseUsesPublicTaskIDAndKeepsUpstreamTaskID(t *testing.T) {
	context, recorder := newTaskContext(t, `{}`)
	adaptor := &TaskAdaptor{}
	response := &http.Response{
		Body: io.NopCloser(strings.NewReader(`{"task_id":"upstream-task","status":"queued"}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelSeedanceFast,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public",
		},
	}

	parsed, taskErr := adaptor.ParseResponse(context, response, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.Equal(t, "upstream-task", parsed.UpstreamTaskID)
	assert.JSONEq(t, `{"task_id":"task_public","status":"queued"}`, string(parsed.TaskData))
	assert.NotContains(t, string(parsed.TaskData), "upstream-task")
	assert.Empty(t, recorder.Body.String())
	var downstream map[string]any
	require.NoError(t, common.Unmarshal(info.PendingResponse, &downstream))
	assert.Equal(t, "task_public", downstream["id"])
	assert.Equal(t, "task_public", downstream["task_id"])
	assert.Equal(t, ModelSeedanceFast, downstream["model"])
	assert.NotContains(t, string(info.PendingResponse), "upstream-task")
}

func TestSanitizeTaskResponseReplacesBothProviderIDFields(t *testing.T) {
	data := (&TaskAdaptor{}).SanitizeTaskResponse(
		[]byte(`{"id":"upstream-id","task_id":"upstream-task","status":"processing"}`),
		"task_public",
	)

	assert.JSONEq(t, `{"id":"task_public","task_id":"task_public","status":"processing"}`, string(data))
	assert.NotContains(t, string(data), "upstream")
}

func TestNativeTaskSubmitResponsePreservesShapeAndHidesUpstreamID(t *testing.T) {
	context, _ := newTaskContext(t, `{}`)
	context.Request.URL.Path = "/v1/tasks"
	adaptor := &TaskAdaptor{}
	response := &http.Response{
		Body: io.NopCloser(strings.NewReader(`{"id":"upstream-task","status":"queued","amount":"3.00"}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelSeedanceFast431,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public",
		},
	}

	parsed, taskErr := adaptor.ParseResponse(context, response, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.Equal(t, "upstream-task", parsed.UpstreamTaskID)
	assert.JSONEq(t, `{"id":"task_public","task_id":"task_public","status":"queued","amount":"3.00"}`, string(info.PendingResponse))
	assert.NotContains(t, string(info.PendingResponse), "upstream-task")
}

func TestConvertToNativeTaskPreservesProviderFieldsAndPublicID(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FailReason: "",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: []byte(`{"task_id":"upstream-task","status":"success","amount":"3.00","actualDuration":10}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToNativeTask(task)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"task_id":"task_public",
		"status":"success",
		"progress":"100%",
		"amount":"3.00",
		"actualDuration":10,
		"metadata":{"url":"https://example.com/result.mp4"}
	}`, string(data))
	assert.NotContains(t, string(data), "upstream-task")
}

func TestConvertToNativeTaskPreservesProviderStatusFamilyAndProgressType(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "generation rejected",
		Data:       []byte(`{"task_id":"task_public","status":"FAILURE","progress":100,"failure_reason":"generation rejected"}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToNativeTask(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "FAILURE", response["status"])
	assert.Equal(t, float64(100), response["progress"])
}

func TestFailureConversionsUseStoredReasonWithoutPublishingResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "upstream authentication failed",
		Data:       []byte(`{"error":{"code":"unauthorized"}}`),
	}

	nativeData, err := (&TaskAdaptor{}).ConvertToNativeTask(task)
	require.NoError(t, err)
	var native map[string]any
	require.NoError(t, common.Unmarshal(nativeData, &native))
	assert.Equal(t, "failed", native["status"])
	assert.Equal(t, "upstream authentication failed", native["failure_reason"])
	assert.NotContains(t, native, "metadata")

	videoData, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var video map[string]any
	require.NoError(t, common.Unmarshal(videoData, &video))
	errorBody, ok := video["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "upstream authentication failed", errorBody["message"])
}

func TestOpenAIVideoQueuedTaskOmitsCompletedAt(t *testing.T) {
	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(&model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusQueued,
		Progress:  "0%",
		CreatedAt: 100,
		UpdatedAt: 200,
	})
	require.NoError(t, err)
	var video map[string]any
	require.NoError(t, common.Unmarshal(data, &video))
	assert.NotContains(t, video, "completed_at")
}

func TestBuildRequestURLAndHeader(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: "https://api.tmlab.store/",
		ApiKey:         "secret",
	}})

	requestURL, err := adaptor.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://api.tmlab.store/v1/tasks", requestURL)

	req := httptest.NewRequest(http.MethodPost, requestURL, nil)
	require.NoError(t, adaptor.BuildRequestHeader(nil, req, nil))
	assert.Equal(t, "Bearer secret", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}
