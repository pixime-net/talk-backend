# Refactorisation de la chaine de composants "conversation"

Objectif: Homogeneiser les consommateurs de messages: langfuseUsageReporter, consoleUsageReporter, MessageStore

## Refactoring struct

```go
type Message struct {
    Role            Role
    Content         string
    ToolCalls       []ToolCall
    ToolResults     []ToolResult
    TurnID          string  // unique turn identifier used for reconciliation across sources (16 bytes)
}
```

```go
type MessageEvent struct {
    Message
    SessionScope SessionScope   // Session and user identifier shared across the CLI session
    Model      Model     // model identifier
    TurnSpanID   string     // 8 bytes turn identifier
    Kind         CallKind   // initial, tool_result
    Usage        Usage      // usage metrics
    StartedAt    time.Time  // When the API call started
    EndedAt      time.Time  // When the API call completed
```

```go
type APICallEvent struct {
    - TraceID      string       // Shared trace ID for the parent turn
    - ParentSpanID string       // SpanID of the parent conversation_turn span
    - OTLPProvider OTLPProvider // LLM provider (anthropic, openai, mistral, _other)
    StartedAt    time.Time    // When the API call started
    EndedAt      time.Time    // When the API call completed
    - Model        string
    - Kind         CallKind
    - Usage        Usage
    Input        string     // The input prompt for this API call
    Output       string     // The response content from the model
    - ToolCalls    []ToolCall // Tool calls made in this API call (if any)
    - SessionID    string     // Session identifier shared across the CLI session
    - UserID       string     // User identifier ("anonymous" until auth is added)
}
```

## Créer une interface MessageEventHandler ayant pour signature

- HandleMessageEvent : prise en compte d'un messageEvent (Question, appel et execution d'outils, Réponse)
- HandleTurnEvent : prise en compte d'un turnEvent (Question et Réponse)

## Créer une classe MessageEventHandlers

- constructeur: liste de liste de MessageEventHandlers
- MessageEventHandlers implémente deux méthodes éponymes HandleMessageEvent et HandleTurnEvent
- Pour chacune méthode: execute en parrallèle toutes les méthodes éponymes du premier élément de la liste, puis passe au deuxième element de la liste, et ainsi de suite (séquence d'activités parrallèles)

## Refactoring de langfuseUsageReporter

- langfuseUsageReporter n'implémente plus UsageReporter mais MessageEventHandler
- les méthodes OnAPICall et OnConversationTurn sont remplacées par HandleMessageEvent et HandleTurnEvent

## Refactoring de consoleUsageReporter

- consoleUsageReporter n'implémente plus UsageReporter mais MessageEventHandler
- les méthodes OnAPICall et OnConversationTurn sont remplacées par HandleMessageEvent et HandleTurnEvent

## Refactoring des implemetation de store inmemory et sqllite

- Elles implémentent maintenant MessageEventHandler
- AddMessage est renommée en HandleMessageEvent, sa signature est modifiée
- AddMessage ne realise plus l'insertion de historyTurn, une nouvelle méthode HandleTurnEvent s'en charge

## Modification de l'interface MessageStore

- AddMessage est supprimée

## Décommissionner UsageReporter

- Supprimer UsageReporter

## Refactoring ConversationManager
