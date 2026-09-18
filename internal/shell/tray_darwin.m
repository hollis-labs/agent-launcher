//go:build darwin

#import <AppKit/AppKit.h>

extern void tachyonTrayTogglePalette(void);
extern void tachyonTrayOpenManager(void);
extern void tachyonTrayOpenSettings(void);
extern void tachyonTrayQuit(void);

static NSString *const TachyonLegacyAutosaveName = @"Item-0";

@interface TachyonTrayController : NSObject <NSMenuDelegate>
@property(nonatomic, strong) NSStatusItem *statusItem;
@property(nonatomic, strong) NSMenu *menu;
@property(nonatomic, strong) id eventMonitor;
@end

@implementation TachyonTrayController

- (void)statusItemClicked:(NSStatusBarButton *)sender {
    NSEvent *event = NSApp.currentEvent;
    BOOL isRightClick = event.type == NSEventTypeRightMouseDown ||
        ((NSEvent.pressedMouseButtons & (1UL << 1)) != 0);
    if (isRightClick) {
        [NSMenu popUpContextMenu:self.menu withEvent:event forView:sender];
        return;
    }
    tachyonTrayTogglePalette();
}

- (void)menuDidClose:(NSMenu *)menu {
    self.statusItem.menu = nil;
    menu.delegate = nil;
}

- (void)openManager:(id)sender {
    tachyonTrayOpenManager();
}

- (void)openSettings:(id)sender {
    tachyonTrayOpenSettings();
}

- (void)quit:(id)sender {
    tachyonTrayQuit();
}

@end

static TachyonTrayController *tachyonTrayController;

static NSString *tachyonPositionKey(NSString *autosaveName) {
    return [@"NSStatusItem Preferred Position " stringByAppendingString:autosaveName];
}

static void tachyonPreparePosition(NSString *autosaveName, double defaultPreferredPosition) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSString *bundleID = NSBundle.mainBundle.bundleIdentifier;
    if (bundleID.length == 0) {
        return;
    }

    // T19 used Wails' automatically assigned Item-0 name. Move only a value
    // persisted in Tachyon's application domain; its registered first-run
    // default was never persistent and therefore is intentionally not copied.
    // This one-time compatibility read preserves a Command-dragged user
    // position while all future load/save behavior belongs to the public
    // NSStatusItem.autosaveName API.
    NSDictionary *persisted = [defaults persistentDomainForName:bundleID];
    NSString *newKey = tachyonPositionKey(autosaveName);
    NSString *oldKey = tachyonPositionKey(TachyonLegacyAutosaveName);
    if (persisted[newKey] == nil && persisted[oldKey] != nil) {
        [defaults setObject:persisted[oldKey] forKey:newKey];
        [defaults removeObjectForKey:oldKey];
    }

    // NSStatusItem has no public initial-position/priority setter. Registering
    // (not writing) the preference AppKit itself uses is therefore the narrow
    // fallback needed on the target notched/crowded menu bar. The stable public
    // autosaveName above removes Item-0 from ongoing behavior, and any
    // persistent named value—migrated or saved by Command-dragging—has normal
    // NSUserDefaults precedence over this fallback.
    [defaults registerDefaults:@{
        newKey: @(defaultPreferredPosition)
    }];
}

static void tachyonOnMainThread(void (^operation)(void)) {
    if (NSThread.isMainThread) {
        operation();
        return;
    }
    dispatch_sync(dispatch_get_main_queue(), operation);
}

void tachyonTrayInstall(char *labelBytes, char *accessibilityLabelBytes,
                        char *autosaveNameBytes, double defaultPreferredPosition) {
    NSString *label = [NSString stringWithUTF8String:labelBytes];
    NSString *accessibilityLabel = [NSString stringWithUTF8String:accessibilityLabelBytes];
    NSString *autosaveName = [NSString stringWithUTF8String:autosaveNameBytes];

    tachyonOnMainThread(^{
        if (tachyonTrayController != nil) {
            return;
        }

        tachyonPreparePosition(autosaveName, defaultPreferredPosition);

        TachyonTrayController *controller = [[TachyonTrayController alloc] init];
        NSStatusItem *statusItem = [NSStatusBar.systemStatusBar
            statusItemWithLength:NSVariableStatusItemLength];
        statusItem.autosaveName = autosaveName;

        NSStatusBarButton *button = statusItem.button;
        button.title = label;
        button.accessibilityLabel = accessibilityLabel;
        button.target = controller;
        button.action = @selector(statusItemClicked:);
        [button sendActionOn:(NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown)];

        NSMenu *menu = [[NSMenu alloc] initWithTitle:accessibilityLabel];
        NSMenuItem *manager = [[NSMenuItem alloc] initWithTitle:@"Open Manager"
            action:@selector(openManager:) keyEquivalent:@""];
        manager.target = controller;
        [menu addItem:manager];
        NSMenuItem *settings = [[NSMenuItem alloc] initWithTitle:@"Settings…"
            action:@selector(openSettings:) keyEquivalent:@""];
        settings.target = controller;
        [menu addItem:settings];
        [menu addItem:NSMenuItem.separatorItem];
        NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"Quit Tachyon"
            action:@selector(quit:) keyEquivalent:@""];
        quit.target = controller;
        [menu addItem:quit];

        controller.statusItem = statusItem;
        controller.menu = menu;
        // A menu assigned permanently would consume left clicks. Attach it
        // only when a right mouse-down reaches this exact status-bar window;
        // AppKit then provides native tracking/highlight and menuDidClose:
        // restores the left-click target/action path.
        __weak TachyonTrayController *weakController = controller;
        controller.eventMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:
            (NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown)
            handler:^NSEvent *(NSEvent *event) {
                TachyonTrayController *strongController = weakController;
                if (strongController == nil ||
                    event.window != strongController.statusItem.button.window) {
                    return event;
                }
                NSStatusBarButton *statusButton = strongController.statusItem.button;
                NSPoint localPoint = [statusButton convertPoint:event.locationInWindow fromView:nil];
                if (!NSPointInRect(localPoint, statusButton.bounds)) {
                    return event;
                }
                BOOL isRightClick = event.type == NSEventTypeRightMouseDown ||
                    ((NSEvent.pressedMouseButtons & (1UL << 1)) != 0);
                if (isRightClick) {
                    strongController.menu.delegate = strongController;
                    strongController.statusItem.menu = strongController.menu;
                }
                return event;
            }];
        tachyonTrayController = controller;
    });
}

int tachyonTrayRemove(void) {
    __block BOOL synchronized = YES;
    tachyonOnMainThread(^{
        if (tachyonTrayController == nil) {
            return;
        }
        if (tachyonTrayController.eventMonitor != nil) {
            [NSEvent removeMonitor:tachyonTrayController.eventMonitor];
            tachyonTrayController.eventMonitor = nil;
        }
        [NSStatusBar.systemStatusBar removeStatusItem:tachyonTrayController.statusItem];
        tachyonTrayController.statusItem = nil;
        tachyonTrayController.menu = nil;
        tachyonTrayController = nil;

        // NSStatusItem persists its autosaved position while it is removed.
        // Flush that AppKit write before Tachyon's shutdown hook returns so an
        // acceptance check that restores a pre-launch preference state cannot
        // later be overwritten by a queued cfprefsd write from this process.
        synchronized = [NSUserDefaults.standardUserDefaults synchronize];
    });
    return synchronized ? 1 : 0;
}

void tachyonTrayPositionWindow(void *nsWindowPointer, int offset) {
    if (nsWindowPointer == NULL || tachyonTrayController == nil) {
        return;
    }
    NSStatusBarButton *button = tachyonTrayController.statusItem.button;
    NSWindow *window = (__bridge NSWindow *)nsWindowPointer;
    NSRect itemFrame = [button.window convertRectToScreen:button.frame];
    NSScreen *screen = button.window.screen ?: NSScreen.mainScreen;
    NSRect windowFrame = window.frame;

    CGFloat x = itemFrame.origin.x + (itemFrame.size.width - windowFrame.size.width) / 2.0;
    x = MIN(x, NSMaxX(screen.frame) - windowFrame.size.width);
    x = MAX(x, NSMinX(screen.frame));

    CGFloat scaledOffset = offset * screen.backingScaleFactor;
    CGFloat y = NSMaxY(screen.visibleFrame) - windowFrame.size.height - scaledOffset;
    windowFrame.origin = NSMakePoint(x, y);
    window.level = NSPopUpMenuWindowLevel;
    [window setFrame:windowFrame display:YES animate:NO];
}
