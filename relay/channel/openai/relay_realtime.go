package openai

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	info.StreamStatus = relaycommon.NewStreamStatus()
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	errChan := make(chan error, 2)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	var responsePending atomic.Bool
	var usageMu sync.Mutex
	reportError := func(err error) {
		// Preserve an error even if a simultaneous normal close wins the select.
		service.MarkConversationCaptureIncomplete(c, "realtime_stream_error")
		select {
		case errChan <- err:
		case <-stop:
		}
	}

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}

	readers.Add(2)
	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in client reader: %v", r))
			}
		}()
		for {
			select {
			case <-stop:
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					select {
					case <-stop:
						return
					default:
					}
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						reportError(fmt.Errorf("error reading from client: %v", err))
						return
					}
					close(clientClosed)
					return
				}
				service.CaptureConversationRealtimeFrame(c, "client_to_upstream", message)

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					reportError(fmt.Errorf("error unmarshalling message: %v", err))
					return
				}

				err = func() error {
					usageMu.Lock()
					defer usageMu.Unlock()
					if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
						if realtimeEvent.Session != nil {
							if realtimeEvent.Session.Tools != nil {
								info.RealtimeTools = realtimeEvent.Session.Tools
							}
						}
					}
					if realtimeEvent.Type == dto.RealtimeEventTypeResponseCreate {
						responsePending.Store(true)
					}

					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						return fmt.Errorf("error counting text token: %w", err)
					}
					logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					localUsage.InputTokens += textToken + audioToken
					localUsage.InputTokenDetails.TextTokens += textToken
					localUsage.InputTokenDetails.AudioTokens += audioToken
					return nil
				}()
				if err != nil {
					reportError(err)
					return
				}

				err = helper.WssString(c, targetConn, string(message))
				if err != nil {
					reportError(fmt.Errorf("error writing to target: %v", err))
					return
				}
			}
		}
	})

	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in target reader: %v", r))
			}
		}()
		for {
			select {
			case <-stop:
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					select {
					case <-stop:
						return
					default:
					}
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						reportError(fmt.Errorf("error reading from target: %v", err))
						return
					}
					close(targetClosed)
					return
				}
				service.CaptureConversationRealtimeFrame(c, "upstream_to_client", message)
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					reportError(fmt.Errorf("error unmarshalling message: %v", err))
					return
				}

				err = func() error {
					usageMu.Lock()
					defer usageMu.Unlock()
					if realtimeEvent.Type == "response.created" {
						responsePending.Store(true)
					}
					if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
						if realtimeEvent.Response == nil {
							return errors.New("response.done is missing response")
						}
						response := realtimeEvent.Response
						switch response.Status {
						case "failed":
							code, errorType := "", ""
							if response.StatusDetails != nil && response.StatusDetails.Error != nil {
								failure := response.StatusDetails.Error
								if failure.Code != nil {
									code = fmt.Sprint(failure.Code)
								}
								errorType = failure.Type
							}
							info.StreamStatus.MarkFailed(code, errorType, 0)
						case "cancelled", "canceled":
							info.StreamStatus.MarkCancelled()
						case "incomplete":
							reason := ""
							if response.StatusDetails != nil {
								reason = response.StatusDetails.Reason
							}
							info.StreamStatus.MarkIncomplete(reason)
						case "", "completed":
							info.StreamStatus.MarkCompleted()
						}
						realtimeUsage := realtimeEvent.Response.Usage
						if realtimeUsage != nil {
							usage.TotalTokens += realtimeUsage.TotalTokens
							usage.InputTokens += realtimeUsage.InputTokens
							usage.OutputTokens += realtimeUsage.OutputTokens
							usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
							usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
							usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
							usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
							usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
							err := preConsumeUsage(c, info, usage, sumUsage)
							if err != nil {
								return fmt.Errorf("error consume usage: %w", err)
							}
							// 本次计费完成，清除
							usage = &dto.RealtimeUsage{}

							localUsage = &dto.RealtimeUsage{}
						} else {
							textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
							if err != nil {
								return fmt.Errorf("error counting text token: %w", err)
							}
							logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
							localUsage.TotalTokens += textToken + audioToken
							info.IsFirstRequest = false
							localUsage.InputTokens += textToken + audioToken
							localUsage.InputTokenDetails.TextTokens += textToken
							localUsage.InputTokenDetails.AudioTokens += audioToken
							err = preConsumeUsage(c, info, localUsage, sumUsage)
							if err != nil {
								return fmt.Errorf("error consume usage: %w", err)
							}
							// 本次计费完成，清除
							localUsage = &dto.RealtimeUsage{}
							// print now usage
						}
						logger.LogInfo(c, common.LogText("realtime streaming sumUsage: %v", sumUsage))
						logger.LogInfo(c, common.LogText("realtime streaming localUsage: %v", localUsage))
						responsePending.Store(false)

					} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
						realtimeSession := realtimeEvent.Session
						if realtimeSession != nil {
							// update audio format
							info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
							info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
						}
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							return fmt.Errorf("error counting text token: %w", err)
						}
						logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsage.TotalTokens += textToken + audioToken
						localUsage.OutputTokens += textToken + audioToken
						localUsage.OutputTokenDetails.TextTokens += textToken
						localUsage.OutputTokenDetails.AudioTokens += audioToken
					}
					return nil
				}()
				if err != nil {
					reportError(err)
					return
				}

				err = helper.WssString(c, clientConn, string(message))
				if err != nil {
					reportError(fmt.Errorf("error writing to client: %v", err))
					return
				}
			}
		}
	})

	select {
	case <-clientClosed:
		if responsePending.Load() {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("client closed during a realtime response"))
		} else {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		}
	case <-targetClosed:
		if responsePending.Load() {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, io.ErrUnexpectedEOF)
		} else {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		}
	case err := <-errChan:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		logger.LogError(c, common.LogText("realtime error: %s", err.Error()))
	case <-c.Request.Context().Done():
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
	}
	// Stop both readers before settling usage and sealing the archive. Otherwise
	// the other reader can append frames after finalization or mutate its totals.
	close(stop)
	_ = clientConn.Close()
	_ = targetConn.Close()
	readers.Wait()

	if usage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, usage, sumUsage)
	}

	if localUsage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, localUsage, sumUsage)
	}

	// check usage total tokens, if 0, use local usage

	return nil, sumUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	// clear usage
	err := service.PreWssConsumeQuota(ctx, info, usage)
	return err
}
