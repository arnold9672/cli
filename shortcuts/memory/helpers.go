// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"net/http"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func callMemoryAPITyped(rctx *common.RuntimeContext, method, apiPath string, body interface{}) (map[string]interface{}, error) {
	req := &larkcore.ApiReq{
		HttpMethod: method,
		ApiPath:    apiPath,
		Body:       body,
	}
	resp, err := rctx.DoAPIWithHeaders(req, memoryPPEHeaders())
	if err != nil {
		return nil, err
	}
	return rctx.ClassifyAPIResponse(resp)
}

func memoryPPEHeaders() http.Header {
	h := make(http.Header)
	h.Set("x-tt-env", memoryTTEnv)
	return h
}

func unwrapMemoryData(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	nested, _ := data["data"].(map[string]interface{})
	if nested == nil {
		return data
	}
	if _, ok := nested["memory_key"]; ok {
		return nested
	}
	if _, ok := nested["memories"]; ok {
		return nested
	}
	return data
}

func firstString(m map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && value != "" {
			return value
		}
	}
	return nil
}
