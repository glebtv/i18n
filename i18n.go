package i18n

import (
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/qor/cache"
	"github.com/qor/cache/memory"
	"github.com/qor/qor"
	"github.com/qor/qor/utils"
	"github.com/theplant/cldr"
)

// Default default locale for i18n
var Default = "en-US"

// I18n struct that hold all translations
type I18n struct {
	scope           string
	value           string
	Backends        []Backend
	FallbackLocales map[string][]string
	fallbackLocales []string
	cacheStore      cache.CacheStoreInterface
}

// ResourceName change display name in qor admin
func (I18n) ResourceName() string {
	return "Translation"
}

// Backend defined methods that needs for translation backend
type Backend interface {
	LoadTranslations() []*Translation
	SaveTranslation(*Translation) error
	DeleteTranslation(*Translation) error
}

// Translation is a struct for translations, including Translation Key, Locale, Value
type Translation struct {
	Key     string
	Locale  string
	Value   string
	Backend Backend `json:"-"`
}

// New initialize I18n with backends
func New(backends ...Backend) *I18n {
	i18n := &I18n{Backends: backends, cacheStore: memory.New()}
	i18n.loadToCacheStore()
	return i18n
}

// SetCacheStore set i18n's cache store
func (i18n *I18n) SetCacheStore(cacheStore cache.CacheStoreInterface) {
	i18n.cacheStore = cacheStore
	i18n.loadToCacheStore()
}

func (i18n *I18n) loadToCacheStore() {
	backends := i18n.Backends
	for i := len(backends) - 1; i >= 0; i-- {
		var backend = backends[i]
		for _, translation := range backend.LoadTranslations() {
			i18n.AddTranslation(translation)
		}
	}
}

// LoadTranslations load translations as map `map[locale]map[key]*Translation`
func (i18n *I18n) LoadTranslations() map[string]map[string]*Translation {
	var translations = map[string]map[string]*Translation{}

	for i := len(i18n.Backends); i > 0; i-- {
		backend := i18n.Backends[i-1]
		for _, translation := range backend.LoadTranslations() {
			if translations[translation.Locale] == nil {
				translations[translation.Locale] = map[string]*Translation{}
			}
			translations[translation.Locale][translation.Key] = translation
		}
	}
	return translations
}

// AddTranslation add translation
func (i18n *I18n) AddTranslation(translation *Translation) error {
	return i18n.cacheStore.Set(cacheKey(translation.Locale, translation.Key), translation)
}

// SaveTranslation save translation
func (i18n *I18n) SaveTranslation(translation *Translation) error {
	for _, backend := range i18n.Backends {
		if backend.SaveTranslation(translation) == nil {
			i18n.AddTranslation(translation)
			return nil
		}
	}

	return errors.New("failed to save translation")
}

// DeleteTranslation delete translation
func (i18n *I18n) DeleteTranslation(translation *Translation) (err error) {
	for _, backend := range i18n.Backends {
		backend.DeleteTranslation(translation)
	}

	return i18n.cacheStore.Delete(cacheKey(translation.Locale, translation.Key))
}

// T translate with locale, key and arguments
func (i18n *I18n) T(locale, key string, args ...interface{}) template.HTML {
	var (
		value           = i18n.value
		translationKey  = key
		fallbackLocales = i18n.fallbackLocales
	)

	if locale == "" {
		locale = Default
	}

	if locales, ok := i18n.FallbackLocales[locale]; ok {
		fallbackLocales = append(fallbackLocales, locales...)
	}
	fallbackLocales = append(fallbackLocales, Default)

	if i18n.scope != "" {
		translationKey = strings.Join([]string{i18n.scope, key}, ".")
	}

	var translation Translation
	if err := i18n.cacheStore.Unmarshal(cacheKey(locale, key), &translation); err != nil || translation.Value == "" {
		for _, fallbackLocale := range fallbackLocales {
			if err := i18n.cacheStore.Unmarshal(cacheKey(fallbackLocale, key), &translation); err == nil && translation.Value != "" {
				break
			}
		}

		if translation.Value == "" {
			// Get default translation if not translated
			if err := i18n.cacheStore.Unmarshal(cacheKey(Default, key), &translation); err != nil || translation.Value == "" {
				// If not initialized
				var defaultBackend Backend
				if len(i18n.Backends) > 0 {
					defaultBackend = i18n.Backends[0]
				}
				translation = Translation{Key: translationKey, Value: value, Locale: locale, Backend: defaultBackend}

				// Save translation
				i18n.SaveTranslation(&translation)
			}
		}
	}

	if translation.Value != "" {
		value = translation.Value
	} else {
		value = key
	}

	if str, err := cldr.Parse(locale, value, args...); err == nil {
		value = str
	}

	return template.HTML(value)
}

// RenderInlineEditAssets render inline edit html, it is using: http://vitalets.github.io/x-editable/index.html
// You could use Bootstrap or JQuery UI by set isIncludeExtendAssetLib to false and load files by yourself
func RenderInlineEditAssets(isIncludeJQuery bool, isIncludeExtendAssetLib bool) (template.HTML, error) {
	for _, gopath := range utils.GOPATH() {
		var content string
		var hasError bool

		if isIncludeJQuery {
			content = `<script src="http://code.jquery.com/jquery-2.0.3.min.js"></script>`
		}

		if isIncludeExtendAssetLib {
			if extendLib, err := ioutil.ReadFile(filepath.Join(gopath, "src/github.com/qor/i18n/views/themes/i18n/inline-edit-libs.tmpl")); err == nil {
				content += string(extendLib)
			} else {
				hasError = true
			}

			if css, err := ioutil.ReadFile(filepath.Join(gopath, "src/github.com/qor/i18n/views/themes/i18n/assets/stylesheets/i18n-inline.css")); err == nil {
				content += fmt.Sprintf("<style>%s</style>", string(css))
			} else {
				hasError = true
			}

		}

		if js, err := ioutil.ReadFile(filepath.Join(gopath, "src/github.com/qor/i18n/views/themes/i18n/assets/javascripts/i18n-inline.js")); err == nil {
			content += fmt.Sprintf("<script type=\"text/javascript\">%s</script>", string(js))
		} else {
			hasError = true
		}

		if !hasError {
			return template.HTML(content), nil
		}
	}

	return template.HTML(""), errors.New("templates not found")
}

func getLocaleFromContext(context *qor.Context) string {
	if locale := utils.GetLocale(context); locale != "" {
		return locale
	}

	return Default
}

type availableLocalesInterface interface {
	AvailableLocales() []string
}

type viewableLocalesInterface interface {
	ViewableLocales() []string
}

type editableLocalesInterface interface {
	EditableLocales() []string
}

func getAvailableLocales(req *http.Request, currentUser qor.CurrentUser) []string {
	if user, ok := currentUser.(viewableLocalesInterface); ok {
		return user.ViewableLocales()
	}

	if user, ok := currentUser.(availableLocalesInterface); ok {
		return user.AvailableLocales()
	}
	return []string{Default}
}

func getEditableLocales(req *http.Request, currentUser qor.CurrentUser) []string {
	if user, ok := currentUser.(editableLocalesInterface); ok {
		return user.EditableLocales()
	}

	if user, ok := currentUser.(availableLocalesInterface); ok {
		return user.AvailableLocales()
	}
	return []string{Default}
}

func cacheKey(strs ...string) string {
	return strings.Join(strs, "/")
}
