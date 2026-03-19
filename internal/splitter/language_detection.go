package splitter

import (
	"path/filepath"
	"strings"

	enry "github.com/go-enry/go-enry/v2"
)

type languageDetectionRequest struct {
	filePath string
	content  []byte
}

type detectedLanguage struct {
	ext string
}

type languageDetector interface {
	Detect(req languageDetectionRequest) *detectedLanguage
}

var defaultLanguageDetector languageDetector = enryLanguageDetector{}

type enryLanguageDetector struct{}

func (enryLanguageDetector) Detect(req languageDetectionRequest) *detectedLanguage {
	if ext := detectedSupportedExtFromPath(req.filePath); ext != "" {
		return &detectedLanguage{ext: ext}
	}

	if len(req.content) == 0 {
		return nil
	}

	for _, languages := range [][]string{
		enry.GetLanguagesByModeline(req.filePath, req.content, nil),
		enry.GetLanguagesByShebang(req.filePath, req.content, nil),
		enry.GetLanguages(req.filePath, req.content),
	} {
		if detected := detectedLanguageFromEnryLanguages(req.filePath, languages); detected != nil {
			return detected
		}
	}

	return nil
}

func resolveLanguage(filePath string) *resolvedLanguage {
	return resolveLanguageForContent(filePath, nil)
}

func resolveLanguageForContent(filePath string, content []byte) *resolvedLanguage {
	detected := defaultLanguageDetector.Detect(languageDetectionRequest{filePath: filePath, content: content})
	if detected == nil {
		return nil
	}

	return resolveLanguageFromExt(detected.ext)
}

func resolvedExt(filePath string) string {
	switch strings.ToLower(filepath.Base(filePath)) {
	case "makefile", "gnumakefile":
		return ".mk"
	case "dockerfile":
		return ".dockerfile"
	case ".gitattributes":
		return ".gitattributes"
	case "go.sum":
		return ".go.sum"
	case "requirements.txt":
		return ".requirements.txt"
	default:
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext != "" {
			return ext
		}

		return ""
	}
}

func detectedSupportedExtFromPath(filePath string) string {
	ext := resolvedExt(filePath)
	if ext == "" || resolveLanguageFromExt(ext) == nil {
		return ""
	}

	return ext
}

func detectedLanguageFromEnryLanguages(filePath string, languages []string) *detectedLanguage {
	for _, language := range languages {
		if ext := supportedExtForEnryLanguage(filePath, language); ext != "" {
			return &detectedLanguage{ext: ext}
		}
	}

	return nil
}

func supportedExtForEnryLanguage(filePath, language string) string {
	pathExt := strings.ToLower(filepath.Ext(filePath))
	if pathExt != "" && resolveLanguageFromExt(pathExt) != nil && enryLanguageSupportsExt(language, pathExt) {
		return pathExt
	}

	bundle := grammarForEnryLanguage(language)
	if bundle.Support != grammarSupportUnmapped && bundle.PrimaryExt != "" {
		return bundle.PrimaryExt
	}

	return ""
}

func enryLanguageSupportsExt(language, ext string) bool {
	for _, candidate := range enry.GetLanguageExtensions(language) {
		if strings.EqualFold(candidate, ext) {
			return true
		}
	}

	return false
}

func resolveLanguageFromExt(ext string) *resolvedLanguage {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return nil
	}

	grammar := grammarForExt(ext)
	if grammar.Support == grammarSupportUnmapped {
		return nil
	}

	return &resolvedLanguage{ext: ext, grammar: grammar}
}
