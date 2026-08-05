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
	db := setupModelListControllerTestDB(t)
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
