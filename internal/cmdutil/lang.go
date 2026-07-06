// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmdutil

import (
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/i18n"
)

// ParseLangFlag validates and canonicalizes a --lang value, shared by config
// and profile so every entry point honors one contract. Empty is unset (no-op);
// a non-empty value must resolve via i18n.Parse or it errors.
func ParseLangFlag(raw string) (i18n.Lang, error) {
	if raw == "" {
		return "", nil
	}
	lang, ok := i18n.Parse(raw)
	if !ok {
		return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"invalid --lang %q; valid values: %s",
			raw, strings.Join(i18n.Codes(), ", ")).
			WithParam("--lang")
	}
	return lang, nil
}
