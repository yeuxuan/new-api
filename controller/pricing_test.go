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
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelAPIDocumentResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		APIDocument string `json:"api_document"`
	} `json:"data"`
}

func TestGetModelAPIDocumentReturnsStoredMarkdown(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	const apiDocument = "# Seedance API\n\nCreate a task with `POST /v1/tasks`."
	require.NoError(t, db.Create(&model.Model{
		ModelName:   "[V2]seedance-2.0",
		APIDocument: apiDocument,
		Status:      1,
		NameRule:    model.NameRuleExact,
	}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/pricing/model_document?model_name="+url.QueryEscape("[V2]seedance-2.0"),
		nil,
	)

	GetModelAPIDocument(context)

	var response modelAPIDocumentResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Empty(t, response.Message)
	assert.Equal(t, apiDocument, response.Data.APIDocument)
}

func TestGetModelAPIDocumentRejectsMissingModelName(t *testing.T) {
	setupModelListControllerTestDB(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/pricing/model_document",
		nil,
	)

	GetModelAPIDocument(context)

	require.JSONEq(t, `{
		"success": false,
		"message": "模型名称不能为空"
	}`, recorder.Body.String())
}
