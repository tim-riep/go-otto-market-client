# go-otto-market-client

Go-Client für die OTTO Market API.

## Verwendung

```go
env := ottomarket.Sandbox // oder ottomarket.Production

tokens := ottomarket.NewClientCredentials(env, clientID, clientSecret,
	[]string{ottomarket.ScopeOrders, ottomarket.ScopeProducts})

client, err := ottomarket.New(env, tokens)
```

| Environment | API | Token-Endpunkt |
|---|---|---|
| `ottomarket.Production` | `https://api.otto.market` | `https://api.otto.market/v1/token` |
| `ottomarket.Sandbox` | `https://sandbox.api.otto.market` | `https://sandbox.api.otto.market/v1/token` |

Aus einer Konfiguration lässt sich die Umgebung mit `ottomarket.ParseEnvironment("sandbox")` lesen;
`Environment` kann auch direkt aus JSON, YAML usw. gelesen werden (`"production"` / `"sandbox"`).

`New` setzt den `Authorization`-Header bei jeder Anfrage aus dem `TokenProvider`. Von außen lässt
sich der Token nicht setzen: Die generierten Params-Structs haben kein `Authorization`-Feld, und
ein über `RequestEditorFn` gesetzter Header wird überschrieben. Tokens werden nur über HTTPS
gesendet.

### Rate Limiting

OTTO erlaubt pro Partner höchstens 20 Anfragen pro Sekunde und 1.000 pro Minute. `New` drosselt
die Anfragen standardmäßig mit einem `MemoryRateLimiter`, der beide Grenzen exakt über ein
gleitendes Zeitfenster einhält. Antwortet die API trotzdem mit 429, wird die Anfrage bis zu
dreimal wiederholt (`Retry-After` wird beachtet, sonst 1 s, 2 s, 4 s).

Das Limit gilt pro Partner, nicht pro Prozess. Laufen mehrere Clients oder Instanzen für denselben
Partner, müssen sie sich einen Limiter teilen, etwa per Redis:

```go
type RateLimiter interface {
	Wait(ctx context.Context, req *http.Request) error // blockiert, bis req gesendet werden darf
}

client, err := ottomarket.New(env, tokens,
	ottomarket.WithRateLimiter(myRedisLimiter), // nil schaltet die Drosselung ab
	ottomarket.WithRetries(5),
)
```

### Beträge

Geldbeträge (`amount` neben `currency`, z. B. Preise, Steuern, Preisnachlässe) haben den Typ
`ottomarket.Amount`. Da die API derzeit nur EUR erlaubt, haben Beträge höchstens zwei
Nachkommastellen und werden exakt in Cent gespeichert statt als Gleitkommazahl:

```go
a, _ := ottomarket.ParseAmount("19.99") // Amount(1999)
a.Cents()                               // 1999
a.String()                              // "19.99"
ottomarket.AmountFromCents(50)          // 0.50
```

Im JSON bleibt ein Betrag eine Zahl (`19.99`). Ein Wert mit mehr als zwei Nachkommastellen
führt beim Dekodieren zu einem Fehler. Alle übrigen Zahlen (Steuersätze, Kennzahlen) sind `float64`.

### Eigener Token

Verwaltet die Anwendung den Token selbst, implementiert sie `TokenProvider`:

```go
type TokenProvider interface {
	Token(ctx context.Context) (string, error) // ohne "Bearer "
}

client, err := ottomarket.New(ottomarket.Production, ottomarket.TokenProviderFunc(func(ctx context.Context) (string, error) {
	return myTokenStore.Current(ctx)
}))
```

### Token-Cache

`NewClientCredentials` holt Tokens per Client-Credentials-Grant und erneuert sie eine Minute vor
Ablauf. Gespeichert werden sie in einem `TokenCache`, standardmäßig im Speicher (`MemoryCache`).
Für Redis, PostgreSQL usw. das Interface implementieren und mit `WithTokenCache` übergeben:

```go
type TokenCache interface {
	Get(ctx context.Context, key string) (*ottomarket.Token, error) // nil, nil wenn nicht vorhanden
	Set(ctx context.Context, key string, token ottomarket.Token) error
}
```

`Token` ist per JSON serialisierbar, `token.ExpiresAt` eignet sich als TTL. Der Schlüssel hängt von
Token-URL, Client-ID und Scopes ab, nicht vom Secret. Fehler des Caches werden zurückgegeben.

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
