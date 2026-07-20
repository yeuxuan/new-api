/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package oaichat

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaymedia "github.com/QuantumNous/new-api/service/relayconvert/internal/media"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesPreservesFileTypes(t *testing.T) {
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(_ *gin.Context, source types.FileSource, _ ...string) (string, string, error) {
			data, ok := source.(*types.Base64Source)
			require.True(t, ok)
			return data.Base64Data, data.MimeType, nil
		},
		GetMimeTypeByExtension: func(ext string) string {
			switch strings.ToLower(ext) {
			case "txt":
				return "text/plain"
			case "pdf":
				return "application/pdf"
			default:
				return "application/octet-stream"
			}
		},
	})
	t.Cleanup(func() {
		relaymedia.SetMediaResolver(relaymedia.MediaResolver{})
	})

	tests := []struct {
		name     string
		fileName string
		content  string
		wantType string
		wantMime string
		wantText string
	}{
		{
			name:     "text file becomes text content",
			fileName: "notes.txt",
			content:  "custom context",
			wantType: "text",
			wantText: "custom context",
		},
		{
			name:     "pdf remains a document",
			fileName: "guide.pdf",
			content:  "%PDF-test",
			wantType: "document",
			wantMime: "application/pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString([]byte(tt.content))
			request := dto.GeneralOpenAIRequest{
				Model: "claude-test",
				Messages: []dto.Message{
					{
						Role: "user",
						Content: []any{
							dto.MediaContent{
								Type: dto.ContentTypeFile,
								File: &dto.MessageFile{
									FileName: tt.fileName,
									FileData: encoded,
								},
							},
						},
					},
				},
			}

			converted, err := OpenAIChatRequestToClaudeMessages(nil, request)
			require.NoError(t, err)
			require.Len(t, converted.Messages, 1)
			parts, ok := converted.Messages[0].Content.([]dto.ClaudeMediaMessage)
			require.True(t, ok)
			require.Len(t, parts, 1)
			assert.Equal(t, tt.wantType, parts[0].Type)

			if tt.wantText != "" {
				require.NotNil(t, parts[0].Text)
				assert.Equal(t, tt.wantText, *parts[0].Text)
			}
			if tt.wantMime != "" {
				require.NotNil(t, parts[0].Source)
				assert.Equal(t, tt.wantMime, parts[0].Source.MediaType)
				assert.Equal(t, encoded, parts[0].Source.Data)
			}
		})
	}
}
