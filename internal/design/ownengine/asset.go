// Package ownengine contains the board engine shared by the runtime and its eval fixture.
package ownengine

import _ "embed"

//go:embed support.js
var Script string
