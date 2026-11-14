// SPDX-License-Identifier: Apache-2.0
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/lang"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed translations/*.json
var translationFiles embed.FS

var translationBundle = sync.OnceValues(func() (*i18n.Bundle, error) {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, code := range languageCodes {
		data, err := translationFiles.ReadFile("translations/" + code + ".json")
		if err != nil {
			return nil, err
		}
		if _, err := bundle.ParseMessageFileBytes(data, code+".json"); err != nil {
			return nil, err
		}
		// Fyne 2.8's Select widget requests this key, which is absent from its
		// bundled catalogs. Add it via the public API before creating widgets.
		var catalog map[string]string
		if err := json.Unmarshal(data, &catalog); err != nil {
			return nil, err
		}
		choice, _ := json.Marshal(map[string]string{"(Select one)": catalog["action.select"]})
		if err := lang.AddTranslationsForLocale(choice, fyne.Locale(code)); err != nil {
			return nil, err
		}
	}
	return bundle, nil
})

// Match NimoteCode: English, Simplified Chinese, Spanish, Japanese, Korean, Russian.
var languageCodes = []string{"en", "zh-Hans", "es", "ja", "ko", "ru"}
var languageNames = []string{"English", "简体中文", "Español", "日本語", "한국어", "Русский"}

type messages struct {
	code      string
	localizer *i18n.Localizer
}

func newMessages(code string) (*messages, error) {
	valid := false
	for _, known := range languageCodes {
		valid = valid || code == known
	}
	if !valid {
		code = "en"
	}
	bundle, err := translationBundle()
	if err != nil {
		return nil, fmt.Errorf("load desktop translations: %w", err)
	}
	return &messages{code: code, localizer: i18n.NewLocalizer(bundle, code, "en")}, nil
}

func (m *messages) t(key string, data ...map[string]any) string {
	config := &i18n.LocalizeConfig{MessageID: key}
	if len(data) > 0 {
		config.TemplateData = data[0]
	}
	value, err := m.localizer.Localize(config)
	if err != nil {
		// English fallback belongs to go-i18n. Tests enforce complete catalogs;
		// missing keys stay visible rather than turning controls into blanks.
		return key
	}
	return value
}
