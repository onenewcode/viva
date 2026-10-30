/* The Dock tile's badge label, the way viva_application_set_badge_label sets it:
 * a label shows at the top corner of the icon, and an empty or NULL one takes it
 * away. A program outside an .app bundle still has a Dock tile, which is the
 * case this checks. */

#import <Cocoa/Cocoa.h>

#include "../dock_badge_darwin.h"

static int failures;

static void expect(NSString *want, const char *what) {
    NSString *got = [[NSApp dockTile] badgeLabel];
    if (want == nil ? got != nil : !(got != nil && [got isEqualToString:want])) {
        fprintf(stderr, "%s: badge = %s, want %s\n", what,
                got == nil ? "(none)" : [got UTF8String],
                want == nil ? "(none)" : [want UTF8String]);
        failures++;
    }
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        if ([NSApp dockTile] == nil) {
            fprintf(stderr, "the application has no Dock tile\n");
            return 1;
        }
        viva_application_set_badge_label("DEV");
        expect(@"DEV", "set");
        viva_application_set_badge_label("");
        expect(nil, "empty clears");
        viva_application_set_badge_label("12");
        expect(@"12", "count");
        viva_application_set_badge_label(NULL);
        expect(nil, "NULL clears");
    }
    return failures == 0 ? 0 : 1;
}
