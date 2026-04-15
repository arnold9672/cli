// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"context"
	"fmt"

	"github.com/larksuite/cli/internal/validate"
	"github.com/larksuite/cli/shortcuts/common"
)

func floatImageBasePath(token, sheetID string) string {
	return fmt.Sprintf("/open-apis/sheets/v3/spreadsheets/%s/sheets/%s/float_images",
		validate.EncodePathSegment(token), validate.EncodePathSegment(sheetID))
}

func floatImageItemPath(token, sheetID, floatImageID string) string {
	return fmt.Sprintf("%s/%s", floatImageBasePath(token, sheetID), validate.EncodePathSegment(floatImageID))
}

func validateFloatImageToken(runtime *common.RuntimeContext) (string, error) {
	token := runtime.Str("spreadsheet-token")
	if u := runtime.Str("url"); u != "" {
		if parsed := extractSpreadsheetToken(u); parsed != u {
			token = parsed
		}
	}
	if token == "" {
		return "", common.FlagErrorf("specify --url or --spreadsheet-token")
	}
	return token, nil
}

func buildFloatImageBody(runtime *common.RuntimeContext, includeToken bool) map[string]interface{} {
	body := map[string]interface{}{}
	if includeToken {
		if s := runtime.Str("float-image-token"); s != "" {
			body["float_image_token"] = s
		}
	}
	if s := runtime.Str("range"); s != "" {
		body["range"] = s
	}
	if runtime.Cmd.Flags().Changed("width") {
		body["width"] = runtime.Str("width")
	}
	if runtime.Cmd.Flags().Changed("height") {
		body["height"] = runtime.Str("height")
	}
	if runtime.Cmd.Flags().Changed("offset-x") {
		body["offset_x"] = runtime.Str("offset-x")
	}
	if runtime.Cmd.Flags().Changed("offset-y") {
		body["offset_y"] = runtime.Str("offset-y")
	}
	return body
}

// SheetCreateFloatImage creates a float image on a sheet.
var SheetCreateFloatImage = common.Shortcut{
	Service:     "sheets",
	Command:     "+create-float-image",
	Description: "Create a floating image on a sheet",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only", "sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "url", Desc: "spreadsheet URL"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token"},
		{Name: "sheet-id", Desc: "sheet ID", Required: true},
		{Name: "float-image-token", Desc: "image file token (from upload API)", Required: true},
		{Name: "range", Desc: "anchor cell (e.g. sheetId!A1:A1)", Required: true},
		{Name: "width", Desc: "width in pixels (>=20)"},
		{Name: "height", Desc: "height in pixels (>=20)"},
		{Name: "offset-x", Desc: "horizontal offset in pixels (>=0)"},
		{Name: "offset-y", Desc: "vertical offset in pixels (>=0)"},
		{Name: "float-image-id", Desc: "custom 10-char alphanumeric ID (auto-generated if omitted)"},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := validateFloatImageToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := validateFloatImageToken(runtime)
		body := buildFloatImageBody(runtime, true)
		if s := runtime.Str("float-image-id"); s != "" {
			body["float_image_id"] = s
		}
		return common.NewDryRunAPI().
			POST("/open-apis/sheets/v3/spreadsheets/:token/sheets/:sheet_id/float_images").
			Body(body).Set("token", token).Set("sheet_id", runtime.Str("sheet-id"))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, _ := validateFloatImageToken(runtime)
		body := buildFloatImageBody(runtime, true)
		if s := runtime.Str("float-image-id"); s != "" {
			body["float_image_id"] = s
		}
		data, err := runtime.CallAPI("POST", floatImageBasePath(token, runtime.Str("sheet-id")), nil, body)
		if err != nil {
			return err
		}
		runtime.Out(data, nil)
		return nil
	},
}

// SheetUpdateFloatImage updates a float image's properties.
var SheetUpdateFloatImage = common.Shortcut{
	Service:     "sheets",
	Command:     "+update-float-image",
	Description: "Update a floating image",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only", "sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "url", Desc: "spreadsheet URL"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token"},
		{Name: "sheet-id", Desc: "sheet ID", Required: true},
		{Name: "float-image-id", Desc: "float image ID", Required: true},
		{Name: "range", Desc: "new anchor cell (e.g. sheetId!B2:B2)"},
		{Name: "width", Desc: "width in pixels (>=20)"},
		{Name: "height", Desc: "height in pixels (>=20)"},
		{Name: "offset-x", Desc: "horizontal offset in pixels (>=0)"},
		{Name: "offset-y", Desc: "vertical offset in pixels (>=0)"},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := validateFloatImageToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := validateFloatImageToken(runtime)
		body := buildFloatImageBody(runtime, false)
		return common.NewDryRunAPI().
			PATCH("/open-apis/sheets/v3/spreadsheets/:token/sheets/:sheet_id/float_images/:float_image_id").
			Body(body).Set("token", token).Set("sheet_id", runtime.Str("sheet-id")).Set("float_image_id", runtime.Str("float-image-id"))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, _ := validateFloatImageToken(runtime)
		body := buildFloatImageBody(runtime, false)
		data, err := runtime.CallAPI("PATCH", floatImageItemPath(token, runtime.Str("sheet-id"), runtime.Str("float-image-id")), nil, body)
		if err != nil {
			return err
		}
		runtime.Out(data, nil)
		return nil
	},
}

// SheetGetFloatImage retrieves a single float image.
var SheetGetFloatImage = common.Shortcut{
	Service:     "sheets",
	Command:     "+get-float-image",
	Description: "Get a floating image by ID",
	Risk:        "read",
	Scopes:      []string{"sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "url", Desc: "spreadsheet URL"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token"},
		{Name: "sheet-id", Desc: "sheet ID", Required: true},
		{Name: "float-image-id", Desc: "float image ID", Required: true},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := validateFloatImageToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := validateFloatImageToken(runtime)
		return common.NewDryRunAPI().
			GET("/open-apis/sheets/v3/spreadsheets/:token/sheets/:sheet_id/float_images/:float_image_id").
			Set("token", token).Set("sheet_id", runtime.Str("sheet-id")).Set("float_image_id", runtime.Str("float-image-id"))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, _ := validateFloatImageToken(runtime)
		data, err := runtime.CallAPI("GET", floatImageItemPath(token, runtime.Str("sheet-id"), runtime.Str("float-image-id")), nil, nil)
		if err != nil {
			return err
		}
		runtime.Out(data, nil)
		return nil
	},
}

// SheetListFloatImages queries all float images in a sheet.
var SheetListFloatImages = common.Shortcut{
	Service:     "sheets",
	Command:     "+list-float-images",
	Description: "List all floating images in a sheet",
	Risk:        "read",
	Scopes:      []string{"sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "url", Desc: "spreadsheet URL"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token"},
		{Name: "sheet-id", Desc: "sheet ID", Required: true},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := validateFloatImageToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := validateFloatImageToken(runtime)
		return common.NewDryRunAPI().
			GET("/open-apis/sheets/v3/spreadsheets/:token/sheets/:sheet_id/float_images/query").
			Set("token", token).Set("sheet_id", runtime.Str("sheet-id"))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, _ := validateFloatImageToken(runtime)
		data, err := runtime.CallAPI("GET", floatImageBasePath(token, runtime.Str("sheet-id"))+"/query", nil, nil)
		if err != nil {
			return err
		}
		runtime.Out(data, nil)
		return nil
	},
}

// SheetDeleteFloatImage deletes a float image.
var SheetDeleteFloatImage = common.Shortcut{
	Service:     "sheets",
	Command:     "+delete-float-image",
	Description: "Delete a floating image",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only", "sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "url", Desc: "spreadsheet URL"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token"},
		{Name: "sheet-id", Desc: "sheet ID", Required: true},
		{Name: "float-image-id", Desc: "float image ID", Required: true},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := validateFloatImageToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := validateFloatImageToken(runtime)
		return common.NewDryRunAPI().
			DELETE("/open-apis/sheets/v3/spreadsheets/:token/sheets/:sheet_id/float_images/:float_image_id").
			Set("token", token).Set("sheet_id", runtime.Str("sheet-id")).Set("float_image_id", runtime.Str("float-image-id"))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, _ := validateFloatImageToken(runtime)
		data, err := runtime.CallAPI("DELETE", floatImageItemPath(token, runtime.Str("sheet-id"), runtime.Str("float-image-id")), nil, nil)
		if err != nil {
			return err
		}
		runtime.Out(data, nil)
		return nil
	},
}
