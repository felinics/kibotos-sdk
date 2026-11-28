"""Incremental decoding of Kibotos exec events."""

from __future__ import annotations

import json
import re
from collections.abc import Iterator
from typing import cast

from .models import ExecEvent, ExecEventName

_EVENT_NAMES = {"started", "stdout", "stderr", "heartbeat", "exited", "error"}
_LINE_END = re.compile(r"\r\n|[\r\n]")


class ExecEventParser:
    def __init__(self) -> None:
        self._pending = ""
        self._skip_lf = False
        self._at_start = True
        self._event = ""
        self._data: list[str] = []

    def feed(self, chunk: str) -> Iterator[ExecEvent]:
        if not chunk:
            return
        if self._at_start:
            chunk = chunk.removeprefix("\ufeff")
            self._at_start = False
        if self._skip_lf:
            chunk = chunk.removeprefix("\n")
            self._skip_lf = False

        text = self._pending + chunk
        offset = 0
        for match in _LINE_END.finditer(text):
            line = text[offset:match.start()]
            offset = match.end()
            self._skip_lf = match.group() == "\r" and offset == len(text)
            event = self._feed_line(line)
            if event is not None:
                yield event
        self._pending = text[offset:]

    def _feed_line(self, line: str) -> ExecEvent | None:
        if not line:
            name, data = self._event, self._data
            self._event, self._data = "", []
            if not data or name not in _EVENT_NAMES:
                return None
            payload = "\n".join(data)
            try:
                decoded = json.loads(payload)
            except ValueError:
                decoded = payload
            return ExecEvent(event=cast(ExecEventName, name), data=decoded)

        field, _, value = line.partition(":")
        value = value.removeprefix(" ")
        if field == "event":
            self._event = value
        elif field == "data":
            self._data.append(value)
        return None
