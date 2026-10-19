// Package locale picks the language of the texts the runtime shows people — the errors and hints its routes answer
// with — by RUNTIME_LOCALE: "en" (the default) or "ru", the language the previous runtime answered in. A deployment
// whose pages show those texts as they were sets RUNTIME_LOCALE=ru and gets them byte for byte.
package locale

import "os"

// Text is one message in every language the runtime speaks.
type Text struct{ en, ru string }

// T is a message with its English and Russian texts.
func T(en, ru string) Text { return Text{en: en, ru: ru} }

// String is the message in the runtime's language.
func (t Text) String() string { return Pick(t.en, t.ru) }

// Russian says whether the runtime answers in Russian. It is read on every call, so a test may switch it.
func Russian() bool { return os.Getenv("RUNTIME_LOCALE") == "ru" }

// Pick is en or ru by the runtime's language, for texts built from parts.
func Pick(en, ru string) string {
	if Russian() {
		return ru
	}
	return en
}
