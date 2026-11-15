// Package web — встроенный статический интерфейс сокращателя.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var files embed.FS

// Static возвращает файловую систему со статикой UI.
func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // невозможная ошибка: каталог вшит на этапе сборки
	}
	return sub
}
