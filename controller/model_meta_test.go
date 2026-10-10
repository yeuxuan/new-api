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
package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func updateModelMetaForTest(t *testing.T, payload map[string]any) {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/models/",
		bytes.NewReader(body),
	)

	UpdateModelMeta(context)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
}

func TestUpdateModelMetaPreservesOmittedAPIDocumentAndClearsExplicitEmptyValue(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	modelMeta := model.Model{
		ModelName:    "documented-model",
		APIDocument:  "# Existing documentation",
		Status:       1,
		SyncOfficial: 1,
		NameRule:     model.NameRuleExact,
	}
	require.NoError(t, db.Create(&modelMeta).Error)

	basePayload := map[string]any{
		"id":            modelMeta.Id,
		"model_name":    modelMeta.ModelName,
		"status":        1,
		"sync_official": 1,
		"name_rule":     model.NameRuleExact,
	}
	updateModelMetaForTest(t, basePayload)

	var stored model.Model
	require.NoError(t, db.First(&stored, modelMeta.Id).Error)
	assert.Equal(t, "# Existing documentation", stored.APIDocument)

	basePayload["api_document"] = ""
	updateModelMetaForTest(t, basePayload)
	require.NoError(t, db.First(&stored, modelMeta.Id).Error)
	assert.Empty(t, stored.APIDocument)
}

func TestModelAPIDocumentDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			t.Cleanup(model.InvalidatePricingCache)

			t.Run("crud_keeps_documents_separate_from_list_metadata", func(t *testing.T) {
				const name = "[V2]seedance-2.0"
				const document = "# Seedance API\n\nPOST https://computetoken.ai/v1/tasks\n\n`image_urls` / `completed` / `metadata.url`"
				var created struct {
					Success bool
					Message string
					Data    model.Model
				}
				modelManagementRequest(t, CreateModelMeta, http.MethodPost, "/api/models/", model.Model{
					ModelName: name, APIDocument: document, Status: 1, SyncOfficial: 1,
				}, &created)
				require.True(t, created.Success, created.Message)
				require.Positive(t, created.Data.Id)
				assert.Equal(t, document, created.Data.APIDocument)

				var listed struct {
					Success bool
					Data    struct{ Items []model.Model }
				}
				modelManagementRequest(t, GetAllModelsMeta, http.MethodGet, "/api/models/", nil, &listed)
				require.True(t, listed.Success)
				require.Len(t, listed.Data.Items, 1)
				assert.Empty(t, listed.Data.Items[0].APIDocument)

				var detail struct {
					Success bool
					Data    model.Model
				}
				modelManagementRequest(t, func(c *gin.Context) {
					c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(created.Data.Id)}}
					GetModelMeta(c)
				}, http.MethodGet, "/api/models/"+strconv.Itoa(created.Data.Id), nil, &detail)
				require.True(t, detail.Success)
				assert.Equal(t, document, detail.Data.APIDocument)

				payload := map[string]any{"id": created.Data.Id, "model_name": name, "description": "Edited metadata", "status": 1, "sync_official": 1}
				updateModelMetaForTest(t, payload)
				var public modelAPIDocumentResponse
				path := "/api/pricing/model_document?model_name=" + url.QueryEscape(name)
				modelManagementRequest(t, GetModelAPIDocument, http.MethodGet, path, nil, &public)
				require.True(t, public.Success, public.Message)
				assert.Equal(t, document, public.Data.APIDocument)

				var result struct{ Success bool }
				modelManagementRequest(t, UpdateModelMeta, http.MethodPut, "/api/models/?status_only=true", map[string]any{"id": created.Data.Id, "status": 0}, &result)
				require.True(t, result.Success)
				modelManagementRequest(t, GetModelAPIDocument, http.MethodGet, path, nil, &public)
				require.True(t, public.Success)
				assert.Empty(t, public.Data.APIDocument)
				var stored model.Model
				require.NoError(t, db.First(&stored, created.Data.Id).Error)
				assert.Equal(t, document, stored.APIDocument)
				assert.Equal(t, "Edited metadata", stored.Description)

				payload["api_document"] = "# Revised document"
				updateModelMetaForTest(t, payload)
				modelManagementRequest(t, GetModelAPIDocument, http.MethodGet, path, nil, &public)
				assert.Equal(t, "# Revised document", public.Data.APIDocument)
				payload["api_document"] = ""
				updateModelMetaForTest(t, payload)
				modelManagementRequest(t, GetModelAPIDocument, http.MethodGet, path, nil, &public)
				assert.Empty(t, public.Data.APIDocument)
			})

			t.Run("sync_preserves_local_document_and_detects_document_edits", func(t *testing.T) {
				local := model.Model{ModelName: "seedance-2.0-fast(431)", APIDocument: "# 431 API\n`referenceImages` / `QUEUED` / `SUCCESS`", Status: 1, SyncOfficial: 1}
				require.NoError(t, local.Insert())
				update := model.MetadataSyncUpdate{
					MetadataSyncSelection: model.MetadataSyncSelection{ModelName: local.ModelName, RecordVersion: model.MetadataRecordVersion(&local, nil, nil), Fields: []string{"description", "tags", "endpoints"}},
					Values:                model.MetadataValues{Description: "Official metadata", Tags: "video", Endpoints: `{"tasks":"/v1/tasks"}`, Status: 1},
				}
				_, err := model.ApplyMetadataSync([]model.MetadataSyncUpdate{update}, nil)
				require.NoError(t, err)
				var stored model.Model
				require.NoError(t, db.First(&stored, local.Id).Error)
				assert.Equal(t, local.APIDocument, stored.APIDocument)
				assert.Equal(t, "Official metadata", stored.Description)
				document, err := model.GetModelAPIDocument(local.ModelName)
				require.NoError(t, err)
				assert.Equal(t, local.APIDocument, document)

				update.RecordVersion = model.MetadataRecordVersion(&stored, nil, nil)
				stored.APIDocument = "# Local revision after preview"
				require.NoError(t, stored.Update())
				_, err = model.ApplyMetadataSync([]model.MetadataSyncUpdate{update}, nil)
				require.ErrorIs(t, err, model.ErrMetadataSyncConflict)
				update.Fields = []string{"api_document"}
				update.RecordVersion = model.MetadataRecordVersion(&stored, nil, nil)
				_, err = model.ApplyMetadataSync([]model.MetadataSyncUpdate{update}, nil)
				require.ErrorContains(t, err, "unsupported metadata field")
				document, err = model.GetModelAPIDocument(local.ModelName)
				require.NoError(t, err)
				assert.Equal(t, stored.APIDocument, document)
			})

			t.Run("catalog_document_and_square_states_share_rule_precedence", func(t *testing.T) {
				for _, record := range []model.Model{
					{ModelName: "doc-", APIDocument: "hidden broad rule", NameRule: model.NameRulePrefix, Status: 0},
					{ModelName: "doc-specific-", APIDocument: "specific", NameRule: model.NameRulePrefix, Status: 1},
					{ModelName: "-suffix", APIDocument: "suffix", NameRule: model.NameRuleSuffix, Status: 1},
					{ModelName: "needle", APIDocument: "contains", NameRule: model.NameRuleContains, Status: 1},
					{ModelName: "doc-specific-hidden", APIDocument: "hidden exact", Status: 0},
					{ModelName: "doc-specific-empty", Status: 1},
				} {
					record.Description = record.APIDocument
					require.NoError(t, record.Insert())
				}
				channel := model.Channel{Name: "Document rules", Key: "fixture", Type: 1, Status: common.ChannelStatusEnabled, Group: "default",
					Models: "doc-specific-one,doc-broad,misc-suffix,misc-needle,doc-specific-hidden,doc-specific-empty,doc-specific-suffix"}
				require.NoError(t, channel.Insert())
				model.RefreshPricing()
				pricing := pricingByModelName(model.GetPricing())
				configured, err := model.GetConfiguredModelChannels()
				require.NoError(t, err)
				connections, err := model.GetModelConnections()
				require.NoError(t, err)
				for _, tc := range []struct {
					name, document string
					visible        bool
				}{
					{"doc-specific-one", "specific", true},
					{"doc-broad", "", false},
					{"misc-suffix", "suffix", true},
					{"misc-needle", "contains", true},
					{"doc-specific-hidden", "", false},
					{"doc-specific-empty", "", true},
					{"doc-specific-suffix", "specific", true},
				} {
					document, err := model.GetModelAPIDocument(tc.name)
					require.NoError(t, err)
					assert.Equal(t, tc.document, document, tc.name)
					item, visible := pricing[tc.name]
					assert.Equal(t, tc.visible, visible, tc.name)
					state := model.ModelSquareHidden
					if tc.visible {
						state = model.ModelSquareVisible
						assert.Equal(t, tc.document, item.Description, tc.name)
					}
					row := &model.Model{ModelName: tc.name}
					require.NoError(t, model.FillModelSquareStates([]*model.Model{row}, configured, connections))
					assert.Equal(t, state, row.SquareState, tc.name)
				}
			})

			t.Run("document_limits_and_vendor_validation_do_not_partially_write", func(t *testing.T) {
				local := model.Model{ModelName: "document-boundary", APIDocument: strings.Repeat("\u6587", model.MaxModelAPIDocumentBytes/3), Status: 0, SyncOfficial: 0}
				require.NoError(t, local.Insert())
				var stored model.Model
				require.NoError(t, db.First(&stored, local.Id).Error)
				assert.Equal(t, local.APIDocument, stored.APIDocument)
				assert.Zero(t, stored.Status)
				assert.Zero(t, stored.SyncOfficial)
				local.APIDocument += "x"
				require.Error(t, local.Update())
				local.APIDocument = "must not be saved"
				local.VendorID = 999999
				require.Error(t, local.Update())
				require.NoError(t, db.First(&stored, local.Id).Error)
				assert.Len(t, stored.APIDocument, model.MaxModelAPIDocumentBytes)
				assert.Zero(t, stored.VendorID)
				oversized := model.Model{ModelName: "oversized-document", APIDocument: strings.Repeat("a", model.MaxModelAPIDocumentBytes+1)}
				require.Error(t, oversized.Insert())
				var count int64
				require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", oversized.ModelName).Count(&count).Error)
				assert.Zero(t, count)
			})
		})
	}
}
