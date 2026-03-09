You are a conversation parser. Analyze the following text content and extract a structured conversation from it.

## Instructions

1. Identify distinct participants/speakers in the text
2. Determine message boundaries (timestamps, speaker labels, paragraph breaks, etc.)
3. Map each participant to a role: "user" or "assistant"
   - If the text is a chat with an AI, the human is "user" and the AI is "assistant"
   - If it's a conversation between two people, the first speaker is "user" and the second is "assistant"
   - For group conversations, map the primary person of interest as "user" and others as "assistant"
4. Extract timestamps if present; otherwise leave empty strings
5. Preserve the original content of each message as faithfully as possible

## Input

**Source**: {{.source}}
**Title**: {{.title}}

**Content**:
```
{{.content}}
```

## Output Format

Return a JSON array of messages. Each message has:
- `role`: "user" or "assistant"
- `content`: the message text
- `timestamp`: RFC3339 timestamp if available, otherwise empty string ""

Example:
```json
[
  {"role": "user", "content": "Hello, how are you?", "timestamp": "2026-01-15T10:00:00Z"},
  {"role": "assistant", "content": "I'm doing well, thanks!", "timestamp": "2026-01-15T10:00:05Z"}
]
```

Return ONLY the JSON array, no other text.
