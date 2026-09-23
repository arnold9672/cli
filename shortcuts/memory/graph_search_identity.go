// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const graphSearchIdentityResponseLimit = 1 << 20

func buildGraphSearchIdentityBody(openID string) map[string]interface{} {
	return map[string]interface{}{
		"out_ids": []string{openID},
		"id_type": graphSearchOpenIDType,
	}
}

func resolveGraphSearchIdentity(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec) (graphSearchSpec, error) {
	userInfo, err := rctx.CallAPITyped(http.MethodGet, graphSearchUserInfoPath, nil, nil)
	if err != nil {
		return graphSearchSpec{}, annotateGraphSearchAuthPreflightError(err)
	}
	openID := strings.TrimSpace(common.GetString(userInfo, "open_id"))
	if !strings.HasPrefix(openID, "ou_") || !graphSearchSafeRootID(openID) {
		return graphSearchSpec{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"user_info did not return a valid open_id")
	}

	internalUserID, err := callGraphSearchIdentityConversion(ctx, rctx, spec, openID)
	if err != nil {
		return graphSearchSpec{}, err
	}
	spec.GraphUserID = openID
	spec.InternalUserID = internalUserID
	return spec, nil
}

func callGraphSearchIdentityConversion(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec, openID string) (int64, error) {
	body, err := json.Marshal(buildGraphSearchIdentityBody(openID))
	if err != nil {
		return 0, errs.NewInternalError(errs.SubtypeSDKError, "failed to marshal Graph Search identity request").WithCause(err)
	}
	if rctx == nil || rctx.Factory == nil || rctx.Factory.HttpClient == nil {
		return 0, errs.NewInternalError(errs.SubtypeSDKError, "Graph Search identity HTTP client is unavailable")
	}
	baseClient, err := rctx.Factory.HttpClient()
	if err != nil {
		return 0, errs.NewNetworkError(errs.SubtypeNetworkTransport, "failed to get Graph Search identity HTTP client").WithCause(err)
	}
	if baseClient == nil {
		return 0, errs.NewInternalError(errs.SubtypeSDKError, "Graph Search identity HTTP client is nil")
	}
	httpClient := *baseClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { //nolint:forbidigo // keep the UAT-derived open_id on the fixed intranet FaaS host.
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphSearchIdentityURL, bytes.NewReader(body)) //nolint:forbidigo // intranet identity bridge is not a Lark OpenAPI endpoint.
	if err != nil {
		return 0, errs.NewInternalError(errs.SubtypeSDKError, "failed to create Graph Search identity request").WithCause(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tt-Env", spec.MemoryTTEnv)
	resp, err := httpClient.Do(req) //nolint:forbidigo // use the injected CLI transport for the fixed intranet FaaS endpoint.
	if err != nil {
		return 0, errs.NewNetworkError(errs.SubtypeNetworkTransport, "Graph Search identity conversion transport failed").WithCause(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, graphSearchIdentityResponseLimit+1))
	if err != nil {
		return 0, errs.NewNetworkError(errs.SubtypeNetworkTransport, "failed to read Graph Search identity response").WithCause(err)
	}
	logID := graphSearchResponseLogID(resp)
	if len(respBody) > graphSearchIdentityResponseLimit {
		return 0, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"Graph Search identity response exceeds %d bytes", graphSearchIdentityResponseLimit).WithLogID(logID)
	}
	payload, decodeErr := decodeGraphSearchJSON(respBody)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, graphSearchIdentityHTTPError(resp.StatusCode, payload, respBody, logID, decodeErr, openID)
	}
	if decodeErr != nil {
		return 0, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"Graph Search identity service returned non-JSON").WithLogID(logID).WithCause(decodeErr)
	}
	if code, message, failed := graphSearchKnowledgeBusinessError(payload); failed {
		message = graphSearchRedactIdentity(message, openID, "")
		return 0, errs.NewAPIError(errs.SubtypeUnknown,
			"Graph Search identity conversion failed: [%d] %s", code, message).WithCode(code).WithLogID(logID)
	}
	mapping := common.GetMap(payload, "out_id_to_in_id_map")
	internalUserID, ok := graphOneHopInt64(mapping[openID])
	if !ok || internalUserID <= 0 {
		return 0, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"Graph Search identity service did not return the current user's internal UID").WithLogID(logID)
	}
	return internalUserID, nil
}

func graphSearchIdentityHTTPError(statusCode int, payload map[string]interface{}, body []byte, logID string, decodeErr error, openID string) error {
	message := strings.TrimSpace(common.GetString(payload, "msg"))
	code, _ := graphOneHopInt64(payload["code"])
	if message == "" {
		message = common.TruncateStr(strings.TrimSpace(string(body)), graphSearchErrorBodyLimit)
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	message = graphSearchRedactIdentity(message, openID, "")
	if statusCode >= http.StatusInternalServerError {
		err := errs.NewNetworkError(errs.SubtypeNetworkServer,
			"Graph Search identity HTTP %d: %s", statusCode, message).WithCode(statusCode).WithLogID(logID)
		if decodeErr != nil {
			return err.WithCause(decodeErr)
		}
		return err
	}
	err := errs.NewAPIError(errs.SubtypeUnknown,
		"Graph Search identity HTTP %d: %s", statusCode, message).WithCode(statusCode).WithLogID(logID)
	if code != 0 {
		err.Code = int(code)
	}
	if decodeErr != nil {
		return err.WithCause(decodeErr)
	}
	return err
}

func graphSearchRedactIdentity(message, openID, internalUserID string) string {
	for _, value := range []string{strings.TrimSpace(openID), strings.TrimSpace(internalUserID)} {
		if value != "" {
			message = strings.ReplaceAll(message, value, "<redacted-user-id>")
		}
	}
	return message
}
