package metrics

import (
	"fmt"
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const (
	directionInbound  = "inbound"
	directionOutbound = "outbound"
)

const (
	typeMessagePrefix = "message:"
	typeNoticePrefix  = "notice:"
	typeRequestPrefix = "request:"
	typeMetaPrefix    = "meta_event:"
	typeOutboundPref  = "outbound:"
)

func InboundTypeKeys(event *zero.Event) []string {
	if event == nil {
		return nil
	}
	switch strings.TrimSpace(event.PostType) {
	case "message":
		return classifyInboundMessage(event.Message)
	case "notice":
		return []string{classifyNoticeType(event)}
	case "request":
		return []string{classifyRequestType(event)}
	case "meta_event":
		return []string{typeMetaPrefix + "other"}
	default:
		return nil
	}
}

func OutboundTypeKeys(payload interface{}) []string {
	switch v := payload.(type) {
	case nil:
		return []string{typeOutboundPref + "other"}
	case string:
		if strings.TrimSpace(v) == "" {
			return []string{typeOutboundPref + "other"}
		}
		return []string{typeOutboundPref + "text"}
	case message.Segment:
		return []string{classifyOutboundSegmentType(v.Type)}
	case *message.Segment:
		if v == nil {
			return []string{typeOutboundPref + "other"}
		}
		return []string{classifyOutboundSegmentType(v.Type)}
	case message.Message:
		return classifyOutboundMessage(v)
	case *message.Message:
		if v == nil {
			return []string{typeOutboundPref + "other"}
		}
		return classifyOutboundMessage(*v)
	case []message.Segment:
		return classifyOutboundMessage(message.Message(v))
	case fmt.Stringer:
		if strings.TrimSpace(v.String()) == "" {
			return []string{typeOutboundPref + "other"}
		}
		return []string{typeOutboundPref + "text"}
	default:
		return []string{typeOutboundPref + "other"}
	}
}

func classifyInboundMessage(msg message.Message) []string {
	if len(msg) == 0 {
		return []string{typeMessagePrefix + "other"}
	}
	keys := make([]string, 0, len(msg))
	for _, seg := range msg {
		keys = append(keys, classifyInboundMessageSegmentType(seg.Type))
	}
	return keys
}

func classifyNoticeType(event *zero.Event) string {
	if event == nil {
		return typeNoticePrefix + "other"
	}
	if strings.TrimSpace(event.DetailType) == "notify" && strings.TrimSpace(event.SubType) == "poke" {
		return typeNoticePrefix + "poke"
	}
	return typeNoticePrefix + "other"
}

func classifyRequestType(event *zero.Event) string {
	if event == nil {
		return typeRequestPrefix + "other"
	}
	switch strings.TrimSpace(event.DetailType) {
	case "friend":
		return typeRequestPrefix + "friend"
	case "group":
		return typeRequestPrefix + "group"
	default:
		return typeRequestPrefix + "other"
	}
}

func classifyInboundMessageSegmentType(raw string) string {
	switch strings.TrimSpace(raw) {
	case "text":
		return typeMessagePrefix + "text"
	case "image":
		return typeMessagePrefix + "image"
	case "video":
		return typeMessagePrefix + "video"
	case "record":
		return typeMessagePrefix + "record"
	case "file":
		return typeMessagePrefix + "file"
	case "at":
		return typeMessagePrefix + "at"
	case "reply":
		return typeMessagePrefix + "reply"
	case "face":
		return typeMessagePrefix + "face"
	case "forward":
		return typeMessagePrefix + "forward"
	case "json":
		return typeMessagePrefix + "json"
	case "xml":
		return typeMessagePrefix + "xml"
	default:
		return typeMessagePrefix + "other"
	}
}

func classifyOutboundMessage(msg message.Message) []string {
	if len(msg) == 0 {
		return []string{typeOutboundPref + "other"}
	}
	keys := make([]string, 0, len(msg))
	for _, seg := range msg {
		keys = append(keys, classifyOutboundSegmentType(seg.Type))
	}
	return keys
}

func classifyOutboundSegmentType(raw string) string {
	switch strings.TrimSpace(raw) {
	case "text":
		return typeOutboundPref + "text"
	case "image":
		return typeOutboundPref + "image"
	case "video":
		return typeOutboundPref + "video"
	case "record":
		return typeOutboundPref + "record"
	case "file":
		return typeOutboundPref + "file"
	case "poke":
		return typeOutboundPref + "poke"
	default:
		return typeOutboundPref + "other"
	}
}
