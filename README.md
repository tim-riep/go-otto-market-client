# go-otto-market-client

Go-Client für die OTTO Market API.

## Client generieren

```bash
go generate ./...
```

Der Generator (`internal/cmd/generate`) lädt die OpenAPI-Spezifikation live von
<https://api.otto.market/docs/openapi.json>, entfernt alle Texte und erzeugt daraus
mit [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) die Datei `client.gen.go`.

Entfernt werden unter anderem `description`, `summary`, `title`, `example`, `examples`,
`externalDocs`, `contact`, `license`, `termsOfService`, `$comment`, Beschreibungen von
OAuth-Scopes und alle `x-*`-Erweiterungen, die oapi-codegen nicht auswertet. Übrig bleibt nur die
technische Struktur: Pfade, Operationen, Parameter, Schemas und Bezeichner. Nach dem Bereinigen
prüft der Generator noch einmal, dass keine Texte übrig sind, und bricht sonst ab.

Die Spezifikation wird nur im Speicher verarbeitet und nie auf die Platte geschrieben.
Sie darf nicht in dieses Repository eingecheckt werden.

Optionen:

```bash
go run ./internal/cmd/generate -url https://api.otto.market/docs/openapi.json -out client.gen.go -package ottomarket
```
