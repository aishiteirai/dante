"""Formato estruturado que o agente devolve (via output_format / JSON Schema)."""

from __future__ import annotations

SEVERITIES = ["critica", "alta", "media", "baixa", "info"]
CATEGORIES = ["bug", "seguranca", "qualidade", "padroes"]

FINDINGS_SCHEMA: dict = {
    "type": "object",
    "additionalProperties": False,
    "required": ["resumo", "achados"],
    "properties": {
        "resumo": {
            "type": "string",
            "description": "2 a 4 frases: o que a mudança faz e a avaliação geral.",
        },
        "achados": {
            "type": "array",
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": [
                    "arquivo", "linha", "severidade", "categoria",
                    "titulo", "descricao", "cenario", "sugestao", "confianca",
                ],
                "properties": {
                    "arquivo": {"type": "string", "description": "Caminho relativo à raiz do repo."},
                    "linha": {"type": "integer", "description": "Linha (no arquivo novo) onde o problema aparece."},
                    "severidade": {"type": "string", "enum": SEVERITIES},
                    "categoria": {"type": "string", "enum": CATEGORIES},
                    "titulo": {"type": "string", "description": "Uma linha, até ~80 caracteres."},
                    "descricao": {"type": "string", "description": "O defeito e por que ele acontece."},
                    "cenario": {
                        "type": "string",
                        "description": "Entrada/estado concreto -> comportamento errado. Vazio só para 'qualidade'.",
                    },
                    "sugestao": {"type": "string", "description": "Correção proposta (pode incluir trecho de código)."},
                    "confianca": {"type": "string", "enum": ["alta", "media", "baixa"]},
                },
            },
        },
    },
}
