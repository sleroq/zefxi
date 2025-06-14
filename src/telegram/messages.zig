// Telegram TDLib Message Parsing
// Contains message parsing functions and MessageContent type
const std = @import("std");
const Allocator = std.mem.Allocator;
const helpers = @import("../helpers.zig");
const TelegramError = @import("telegram.zig").TelegramError;
const AttachmentInfo = @import("telegram.zig").AttachmentInfo;

/// MessageContent may own heap-allocated fields. If it does, the owner must call deinit on the contained AttachmentInfo.
/// All parse*Message functions return MessageContent values that may own heap memory; caller is responsible for cleanup.
pub const MessageContent = union(enum) {
    text: []const u8,
    attachment: AttachmentInfo,
    text_with_attachment: struct {
        text: []const u8,
        attachment: AttachmentInfo,
    },
};

pub fn extractCaption(content_obj: std.json.ObjectMap) ?[]const u8 {
    if (content_obj.get("caption")) |caption_obj| {
        const caption_text_obj = caption_obj.object;
        if (caption_text_obj.get("text")) |caption_text| {
            return caption_text.string;
        }
    }
    return null;
}

pub fn parsePhotoMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("photo")) |photo| {
        const photo_obj = photo.object;
        if (photo_obj.get("sizes")) |sizes| {
            const sizes_array = sizes.array;
            if (sizes_array.items.len > 0) {
                const largest_size = sizes_array.items[sizes_array.items.len - 1].object;
                if (largest_size.get("photo")) |size_photo| {
                    const size_photo_obj = size_photo.object;
                    const file_id = try helpers.jsonGetI64(size_photo_obj, "id", allocator);
                    const width = try helpers.jsonGetI32(largest_size, "width", allocator);
                    const height = try helpers.jsonGetI32(largest_size, "height", allocator);
                    const attachment_info = AttachmentInfo{
                        .file_id = file_id,
                        .attachment_type = .photo,
                        .width = width,
                        .height = height,
                        .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
                        .local_path = null,
                        .url = null,
                    };
                    try startFileDownload(file_id, chat_id, user_id);
                    if (caption) |cap| {
                        return MessageContent{ .text_with_attachment = .{
                            .text = cap,
                            .attachment = attachment_info,
                        } };
                    } else {
                        return MessageContent{ .attachment = attachment_info };
                    }
                }
            }
        }
    }
    return null;
}

pub fn parseDocumentMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("document")) |document| {
        const document_obj = document.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (document_obj.get("document")) |doc_file| {
            const doc_file_obj = doc_file.object;
            file_id = try helpers.jsonGetI64(doc_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(doc_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const file_name = helpers.jsonGetString(document_obj, "file_name", allocator) catch null;
        const mime_type = helpers.jsonGetString(document_obj, "mime_type", allocator) catch null;
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .document,
            .file_size = file_size,
            .file_name = if (file_name) |name| try allocator.dupe(u8, name) else null,
            .mime_type = if (mime_type) |mime| try allocator.dupe(u8, mime) else null,
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}

pub fn parseVideoMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("video")) |video| {
        const video_obj = video.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (video_obj.get("video")) |video_file| {
            const video_file_obj = video_file.object;
            file_id = try helpers.jsonGetI64(video_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(video_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const width = try helpers.jsonGetI32(video_obj, "width", allocator);
        const height = try helpers.jsonGetI32(video_obj, "height", allocator);
        const duration = helpers.jsonGetI32(video_obj, "duration", allocator) catch 0;
        const file_name = helpers.jsonGetString(video_obj, "file_name", allocator) catch null;
        const mime_type = helpers.jsonGetString(video_obj, "mime_type", allocator) catch null;
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .video,
            .width = width,
            .height = height,
            .duration = duration,
            .file_size = file_size,
            .file_name = if (file_name) |name| try allocator.dupe(u8, name) else null,
            .mime_type = if (mime_type) |mime| try allocator.dupe(u8, mime) else null,
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}

pub fn parseAudioMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("audio")) |audio| {
        const audio_obj = audio.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (audio_obj.get("audio")) |audio_file| {
            const audio_file_obj = audio_file.object;
            file_id = try helpers.jsonGetI64(audio_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(audio_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const duration = helpers.jsonGetI32(audio_obj, "duration", allocator) catch 0;
        const file_name = helpers.jsonGetString(audio_obj, "file_name", allocator) catch null;
        const mime_type = helpers.jsonGetString(audio_obj, "mime_type", allocator) catch null;
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .audio,
            .duration = duration,
            .file_size = file_size,
            .file_name = if (file_name) |name| try allocator.dupe(u8, name) else null,
            .mime_type = if (mime_type) |mime| try allocator.dupe(u8, mime) else null,
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}

pub fn parseVoiceMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("voice_note")) |voice| {
        const voice_obj = voice.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (voice_obj.get("voice")) |voice_file| {
            const voice_file_obj = voice_file.object;
            file_id = try helpers.jsonGetI64(voice_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(voice_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const duration = helpers.jsonGetI32(voice_obj, "duration", allocator) catch 0;
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .voice,
            .duration = duration,
            .file_size = file_size,
            .mime_type = try allocator.dupe(u8, "audio/ogg"),
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}

pub fn parseVideoNoteMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("video_note")) |video_note| {
        const video_note_obj = video_note.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (video_note_obj.get("video")) |video_file| {
            const video_file_obj = video_file.object;
            file_id = try helpers.jsonGetI64(video_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(video_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const duration = helpers.jsonGetI32(video_note_obj, "duration", allocator) catch 0;
        const length = try helpers.jsonGetI32(video_note_obj, "length", allocator);
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .video_note,
            .width = length,
            .height = length,
            .duration = duration,
            .file_size = file_size,
            .mime_type = try allocator.dupe(u8, "video/mp4"),
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}

pub fn parseStickerMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    if (content_obj.get("sticker")) |sticker| {
        const sticker_obj = sticker.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (sticker_obj.get("sticker")) |sticker_file| {
            const sticker_file_obj = sticker_file.object;
            file_id = try helpers.jsonGetI64(sticker_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(sticker_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const width = try helpers.jsonGetI32(sticker_obj, "width", allocator);
        const height = try helpers.jsonGetI32(sticker_obj, "height", allocator);
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .sticker,
            .width = width,
            .height = height,
            .file_size = file_size,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        return MessageContent{ .attachment = attachment_info };
    }
    return null;
}

pub fn parseAnimationMessage(
    allocator: Allocator,
    content_obj: std.json.ObjectMap,
    chat_id: i64,
    user_id: i64,
    startFileDownload: fn (file_id: i64, chat_id: i64, user_id: i64) anyerror!void,
) !?MessageContent {
    const caption = extractCaption(content_obj);
    if (content_obj.get("animation")) |animation| {
        const animation_obj = animation.object;
        var file_id: i64 = 0;
        var file_size: i64 = 0;
        if (animation_obj.get("animation")) |animation_file| {
            const animation_file_obj = animation_file.object;
            file_id = try helpers.jsonGetI64(animation_file_obj, "id", allocator);
            file_size = try helpers.jsonGetI64(animation_file_obj, "size", allocator);
        } else {
            return TelegramError.MissingAttachmentData;
        }
        const width = try helpers.jsonGetI32(animation_obj, "width", allocator);
        const height = try helpers.jsonGetI32(animation_obj, "height", allocator);
        const duration = helpers.jsonGetI32(animation_obj, "duration", allocator) catch 0;
        const file_name = helpers.jsonGetString(animation_obj, "file_name", allocator) catch null;
        const mime_type = helpers.jsonGetString(animation_obj, "mime_type", allocator) catch null;
        const attachment_info = AttachmentInfo{
            .file_id = file_id,
            .attachment_type = .animation,
            .width = width,
            .height = height,
            .duration = duration,
            .file_size = file_size,
            .file_name = if (file_name) |name| try allocator.dupe(u8, name) else null,
            .mime_type = if (mime_type) |mime| try allocator.dupe(u8, mime) else null,
            .caption = if (caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = null,
            .url = null,
        };
        try startFileDownload(file_id, chat_id, user_id);
        if (caption) |cap| {
            return MessageContent{ .text_with_attachment = .{
                .text = cap,
                .attachment = attachment_info,
            } };
        } else {
            return MessageContent{ .attachment = attachment_info };
        }
    }
    return null;
}
