// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
)

// maxAttrs bounds the attributes kept per record, groups expanded.
const maxAttrs = 64

// handler copies every record it handles into the ring, redacted, then passes the
// original record on unchanged.
type handler struct {
	ring   *Ring
	next   slog.Handler
	source string
	prefix string // open group path, "a.b." or ""
	attrs  []Attr // attributes added with WithAttrs, already redacted
}

// Wrap returns next decorated so every record it handles is also stored in r under
// source.
func (r *Ring) Wrap(next slog.Handler, source string) slog.Handler {
	return &handler{ring: r, next: next, source: source}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	attrs := slices.Clip(h.attrs)
	rec.Attrs(func(a slog.Attr) bool {
		attrs = appendFlat(attrs, h.prefix, a)
		return len(attrs) < maxAttrs
	})
	h.ring.Add(ctx, Record{
		Time: rec.Time, Level: rec.Level, Source: h.source,
		Message: redactText(rec.Message, maxMessageBytes), Attrs: attrs,
	})
	if err := h.next.Handle(ctx, rec); err != nil {
		return fmt.Errorf("logring: next handler: %w", err)
	}
	return nil
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = slices.Clone(h.attrs)
	for _, a := range as {
		c.attrs = appendFlat(c.attrs, h.prefix, a)
	}
	c.next = h.next.WithAttrs(as)
	return &c
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.prefix = h.prefix + name + "."
	c.next = h.next.WithGroup(name)
	return &c
}

// appendFlat appends a, groups expanded into dotted keys, without recursion and
// without exceeding maxAttrs.
func appendFlat(dst []Attr, prefix string, a slog.Attr) []Attr {
	type item struct {
		prefix string
		attr   slog.Attr
	}
	stack := []item{{prefix, a}}
	for steps := 0; len(stack) > 0 && len(dst) < maxAttrs && steps < 4*maxAttrs; steps++ {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		v := it.attr.Value.Resolve()
		if v.Kind() != slog.KindGroup {
			if it.attr.Key != "" {
				key := it.prefix + it.attr.Key
				dst = append(dst, Attr{Key: key, Value: redactAttr(key, v.String())})
			}
			continue
		}
		p := it.prefix
		if it.attr.Key != "" {
			p += it.attr.Key + "."
		}
		group := v.Group()
		for i := len(group) - 1; i >= 0 && len(stack) < maxAttrs; i-- {
			stack = append(stack, item{p, group[i]})
		}
	}
	return dst
}
