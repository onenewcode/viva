#include "file_icon_darwin.h"

#import <AppKit/AppKit.h>

#include <string.h>

static void viva_file_icon_fail(char **error_message, const char *message) {
    if (error_message != NULL) {
        *error_message = strdup(message);
    }
}

int viva_file_icon_bgra(const char *path, int pixels, unsigned char *out, char **error_message) {
    if (path == NULL || out == NULL || pixels <= 0) {
        viva_file_icon_fail(error_message, "file icon request is incomplete");
        return -1;
    }
    @autoreleasepool {
        NSString *file = [NSString stringWithUTF8String:path];
        if (file == nil) {
            viva_file_icon_fail(error_message, "file icon path is not valid UTF-8");
            return -1;
        }
        NSImage *icon = [NSWorkspace.sharedWorkspace iconForFile:file];
        if (icon == nil) {
            viva_file_icon_fail(error_message, "the system has no icon for the file");
            return -1;
        }
        memset(out, 0, (size_t)pixels * (size_t)pixels * 4);
        CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        CGContextRef context = CGBitmapContextCreate(out, (size_t)pixels, (size_t)pixels, 8, (size_t)pixels * 4, space,
                                                     kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
        CGColorSpaceRelease(space);
        if (context == NULL) {
            viva_file_icon_fail(error_message, "failed to create the icon bitmap");
            return -1;
        }
        NSGraphicsContext *graphics = [NSGraphicsContext graphicsContextWithCGContext:context flipped:NO];
        [NSGraphicsContext saveGraphicsState];
        NSGraphicsContext.currentContext = graphics;
        /* The context has no scale, so a rectangle of pixels x pixels points is
         * pixels x pixels device pixels and the icon picks the representation
         * made for that size. */
        [icon drawInRect:NSMakeRect(0, 0, pixels, pixels)
                fromRect:NSZeroRect
               operation:NSCompositingOperationSourceOver
                fraction:1.0
          respectFlipped:YES
                   hints:@{NSImageHintInterpolation : @(NSImageInterpolationHigh)}];
        [NSGraphicsContext restoreGraphicsState];
        CGContextRelease(context);
    }
    return 0;
}
