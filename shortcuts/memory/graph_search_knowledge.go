// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const graphSearchErrorBodyLimit = 4000

type graphSearchCandidate struct {
	Rank               int     `json:"rank"`
	PassageID          string  `json:"passage_id,omitempty"`
	SourceType         int64   `json:"source_type"`
	Title              string  `json:"title,omitempty"`
	Content            string  `json:"content,omitempty"`
	URL                string  `json:"url,omitempty"`
	Score              float64 `json:"score,omitempty"`
	CreateTimeSec      int64   `json:"create_time_sec,omitempty"`
	UpdateTimeSec      int64   `json:"update_time_sec,omitempty"`
	MinutesID          string  `json:"minutes_id,omitempty"`
	MinuteToken        string  `json:"minute_token,omitempty"`
	PermissionFiltered bool    `json:"permission_filtered"`
}

func buildGraphSearchKnowledgeBody(query string, userID interface{}) map[string]interface{} {
	return map[string]interface{}{
		"Head": map[string]interface{}{
			"TenantID": graphSearchTenantID,
			"Auth": map[string]interface{}{
				"UserID": userID,
			},
		},
		"query":           query,
		"knowledge_scope": "enterprise",
		"enterprise_knowledge_source": map[string]interface{}{
			"space":   map[string]interface{}{"searchable": true},
			"wiki":    map[string]interface{}{"searchable": true},
			"message": map[string]interface{}{"searchable": true},
			"minutes": map[string]interface{}{"searchable": true},
		},
	}
}

func buildGraphSearchKnowledgeHeaders(userID, ttEnv string) map[string]string {
	return map[string]string{
		"Content-Type":          "application/json",
		"X-Tt-Env":              ttEnv,
		"Rpc-Transit-TENANT-ID": strconv.FormatInt(graphSearchTenantID, 10),
		"Rpc-Transit-USER-ID":   userID,
		"Rpc-Transit-APP-ID":    strconv.FormatInt(graphSearchAppID, 10),
		"Rpc-Persist-TENANT-ID": strconv.FormatInt(graphSearchTenantID, 10),
		"Rpc-Persist-USER-ID":   userID,
		"Rpc-Persist-APP-ID":    strconv.FormatInt(graphSearchAppID, 10),
	}
}

func callGraphSearchKnowledge(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec) ([]graphSearchCandidate, string, error) {
	release, err := acquireGraphSearchRequestPermit(ctx, spec)
	if err != nil {
		return nil, "", errs.NewNetworkError(errs.SubtypeNetworkTransport, "Knowledge QA canceled before request").WithCause(err)
	}
	defer release()
	body, err := json.Marshal(buildGraphSearchKnowledgeBody(spec.Query, spec.InternalUserID))
	if err != nil {
		return nil, "", errs.NewInternalError(errs.SubtypeSDKError, "failed to marshal Knowledge QA request").WithCause(err)
	}
	if rctx == nil || rctx.Factory == nil || rctx.Factory.HttpClient == nil {
		return nil, "", errs.NewInternalError(errs.SubtypeSDKError, "Knowledge QA HTTP client is unavailable")
	}
	baseClient, err := rctx.Factory.HttpClient()
	if err != nil {
		return nil, "", errs.NewNetworkError(errs.SubtypeNetworkTransport, "failed to get Knowledge QA HTTP client").WithCause(err)
	}
	if baseClient == nil {
		return nil, "", errs.NewInternalError(errs.SubtypeSDKError, "Knowledge QA HTTP client is nil")
	}
	httpClient := *baseClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { //nolint:forbidigo // block redirects so RPC identity headers never leave the fixed FaaS host.
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphSearchKnowledgeQAURL, bytes.NewReader(body)) //nolint:forbidigo // intranet FaaS is not a Lark OpenAPI endpoint.
	if err != nil {
		return nil, "", errs.NewInternalError(errs.SubtypeSDKError, "failed to create Knowledge QA request").WithCause(err)
	}
	for key, value := range buildGraphSearchKnowledgeHeaders(strconv.FormatInt(spec.InternalUserID, 10), spec.MemoryTTEnv) {
		req.Header.Set(key, value)
	}

	resp, err := httpClient.Do(req) //nolint:forbidigo // the injected client preserves CLI security transports for this intranet-only FaaS call.
	if err != nil {
		return nil, "", errs.NewNetworkError(errs.SubtypeNetworkTransport, "Knowledge QA transport failed").WithCause(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, graphSearchResponseLogID(resp), errs.NewNetworkError(errs.SubtypeNetworkTransport,
			"failed to read Knowledge QA response").WithCause(err)
	}

	logID := graphSearchResponseLogID(resp)
	payload, decodeErr := decodeGraphSearchJSON(respBody)
	uidText := strconv.FormatInt(spec.InternalUserID, 10)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, logID, graphSearchKnowledgeHTTPError(resp.StatusCode, payload, respBody, logID, decodeErr, uidText)
	}
	if decodeErr != nil {
		return nil, logID, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"Knowledge QA returned non-JSON").WithLogID(logID).WithCause(decodeErr)
	}
	if code, message, failed := graphSearchKnowledgeBusinessError(payload); failed {
		message = graphSearchRedactUID(message, uidText)
		return nil, logID, errs.NewAPIError(errs.SubtypeUnknown,
			"Knowledge QA failed: [%d] %s", code, message).WithCode(code).WithLogID(logID)
	}
	candidates, err := parseGraphSearchCandidates(payload)
	if err != nil {
		return nil, logID, err
	}
	return candidates, logID, nil
}

func decodeGraphSearchJSON(body []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]interface{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func graphSearchKnowledgeHTTPError(statusCode int, payload map[string]interface{}, body []byte, logID string, decodeErr error, uidText string) error {
	message := strings.TrimSpace(common.GetString(payload, "msg"))
	code, _ := graphOneHopInt64(payload["code"])
	if message == "" {
		message = common.TruncateStr(strings.TrimSpace(string(body)), graphSearchErrorBodyLimit)
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	message = graphSearchRedactUID(message, uidText)

	if statusCode >= 500 {
		err := errs.NewNetworkError(errs.SubtypeNetworkServer,
			"Knowledge QA HTTP %d: %s", statusCode, message).WithCode(statusCode).WithLogID(logID)
		if decodeErr != nil {
			return err.WithCause(decodeErr)
		}
		return err
	}
	err := errs.NewAPIError(errs.SubtypeUnknown,
		"Knowledge QA HTTP %d: %s", statusCode, message).WithCode(statusCode).WithLogID(logID)
	if code != 0 {
		err.Code = int(code)
	}
	if decodeErr != nil {
		return err.WithCause(decodeErr)
	}
	return err
}

func graphSearchRedactUID(message, uidText string) string {
	return graphSearchRedactIdentity(message, "", uidText)
}

func graphSearchKnowledgeBusinessError(payload map[string]interface{}) (int, string, bool) {
	if code, ok := graphOneHopInt64(payload["code"]); ok && code != 0 {
		message := strings.TrimSpace(common.GetString(payload, "msg"))
		if message == "" {
			message = "unknown error"
		}
		return int(code), message, true
	}
	baseResp := common.GetMap(payload, "BaseResp")
	if len(baseResp) == 0 {
		return 0, "", false
	}
	code, ok := graphOneHopInt64(baseResp["StatusCode"])
	if !ok || code == 0 {
		return 0, "", false
	}
	message := strings.TrimSpace(common.GetString(baseResp, "StatusMessage"))
	if message == "" {
		message = "unknown error"
	}
	return int(code), message, true
}

func parseGraphSearchCandidates(payload map[string]interface{}) ([]graphSearchCandidate, error) {
	raw, exists := payload["passages"]
	if !exists || raw == nil {
		return []graphSearchCandidate{}, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"Knowledge QA response passages must be a list")
	}
	candidates := make([]graphSearchCandidate, 0, len(items))
	for index, item := range items {
		passage, ok := item.(map[string]interface{})
		if !ok {
			return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
				"Knowledge QA response passages[%d] must be an object", index)
		}
		extra := common.GetMap(passage, "extra")
		sourceType, _ := graphOneHopInt64(passage["source_type"])
		candidate := graphSearchCandidate{
			Rank:               index + 1,
			PassageID:          strings.TrimSpace(common.GetString(passage, "id")),
			SourceType:         sourceType,
			Title:              strings.TrimSpace(common.GetString(passage, "title")),
			Content:            strings.TrimSpace(common.GetString(passage, "content")),
			URL:                strings.TrimSpace(common.GetString(passage, "url")),
			Score:              common.GetFloat(passage, "score"),
			CreateTimeSec:      parseGraphSearchTimestamp(extra["create_time"]),
			UpdateTimeSec:      parseGraphSearchTimestamp(extra["update_time"]),
			PermissionFiltered: true,
		}
		candidate.MinutesID = graphSearchTransparentInfoString(passage["transparent_info"], "minutes_id")
		candidate.MinuteToken, _ = graphSearchURLPathToken(candidate.URL, "/minutes/")
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func parseGraphSearchTimestamp(value interface{}) int64 {
	var parsed int64
	switch value := value.(type) {
	case string:
		parsed, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	default:
		parsed, _ = graphOneHopInt64(value)
	}
	if parsed <= 0 {
		return 0
	}
	if parsed >= 100_000_000_000 {
		parsed /= 1000
	}
	return parsed
}

func graphSearchTransparentInfoString(value interface{}, key string) string {
	switch value := value.(type) {
	case map[string]interface{}:
		return strings.TrimSpace(common.GetString(value, key))
	case string:
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			return ""
		}
		return strings.TrimSpace(common.GetString(decoded, key))
	default:
		return ""
	}
}

func graphSearchURLPathToken(rawURL string, prefixes ...string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(parsed.Path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(parsed.Path, prefix)
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			rest = rest[:slash]
		}
		rest = strings.TrimSpace(rest)
		if graphSearchSafeRootID(rest) {
			return rest, true
		}
	}
	return "", false
}

func graphSearchMessageChatID(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	chatID := strings.TrimSpace(parsed.Query().Get("chatId"))
	if chatID == "" {
		chatID = strings.TrimSpace(parsed.Query().Get("chatid"))
	}
	if !graphSearchPositiveDecimal(chatID) {
		return "", false
	}
	return chatID, true
}

func graphSearchSafeRootID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func graphSearchPositiveDecimal(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0
}

func graphSearchResponseLogID(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	for _, key := range []string{"x-tt-logid", "x-bytefaas-request-id"} {
		if value := strings.TrimSpace(resp.Header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}
