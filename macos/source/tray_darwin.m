//go:build darwin && !wfbridge

#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

static NSStatusItem *wfStatusItem = nil;
static NSMenu *wfStatusMenu = nil;

@interface WFTrayController : NSObject
- (void)showMainWindow:(id)sender;
- (void)quitApplication:(id)sender;
- (void)statusItemClicked:(id)sender;
- (void)applicationDidHide:(NSNotification *)notification;
@end

@implementation WFTrayController

- (void)showMainWindow:(id)sender {
    wfTrayShowWindow();
}

- (void)quitApplication:(id)sender {
    wfTrayQuitApplication();
}

- (void)statusItemClicked:(id)sender {
    NSEvent *event = [NSApp currentEvent];
    if ([event type] == NSEventTypeRightMouseUp || [event type] == NSEventTypeRightMouseDown) {
        [wfStatusItem popUpStatusItemMenu:wfStatusMenu];
        return;
    }
    wfTrayShowWindow();
}

- (void)applicationDidHide:(NSNotification *)notification {
    // Wails implements HideWindowOnClose as NSApp hide:. Convert that close
    // gesture into a normal minimisation so the application stays visible in
    // the Dock while the independent status-item menu remains available.
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        [NSApp unhideWithoutActivation];
        NSWindow *window = [NSApp mainWindow];
        if (window == nil) {
            for (NSWindow *candidate in [NSApp windows]) {
                if ([candidate isVisible]) {
                    window = candidate;
                    break;
                }
            }
        }
        if (window != nil && ![window isMiniaturized]) {
            [window miniaturize:nil];
        }
    });
}

@end

static WFTrayController *wfTrayController = nil;

void wfTrayStart(const unsigned char *iconBytes, int iconLength) {
    if (iconBytes == NULL || iconLength <= 0) {
        return;
    }

    // Copy before dispatching so no Go memory remains referenced after this
    // cgo call returns.
    NSData *iconData = [[NSData alloc] initWithBytes:iconBytes length:(NSUInteger)iconLength];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (wfStatusItem == nil) {
            // A variable-length item created before its image is assigned can
            // receive a zero-width slot on recent macOS releases. The icon is
            // deliberately square, so reserve one status-bar square up front.
            wfStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
            wfTrayController = [[WFTrayController alloc] init];
            [[NSNotificationCenter defaultCenter] addObserver:wfTrayController
                                                     selector:@selector(applicationDidHide:)
                                                         name:NSApplicationDidHideNotification
                                                       object:NSApp];

            wfStatusMenu = [[NSMenu alloc] initWithTitle:@"XIASS Tools"];
            NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:@"打开主界面"
                                                               action:@selector(showMainWindow:)
                                                        keyEquivalent:@""];
            [showItem setTarget:wfTrayController];
            [wfStatusMenu addItem:showItem];
            [wfStatusMenu addItem:[NSMenuItem separatorItem]];

            NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"退出 XIASS Tools"
                                                               action:@selector(quitApplication:)
                                                        keyEquivalent:@""];
            [quitItem setTarget:wfTrayController];
            [wfStatusMenu addItem:quitItem];

            NSStatusBarButton *button = [wfStatusItem button];
            [button setTarget:wfTrayController];
            [button setAction:@selector(statusItemClicked:)];
            [button sendActionOn:(NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp)];
            [button setAccessibilityLabel:@"XIASS Tools"];
        }

        NSImage *image = [[NSImage alloc] initWithData:iconData];
        if (image != nil) {
            [image setSize:NSMakeSize(18.0, 18.0)];
            [image setTemplate:NO];
            [[wfStatusItem button] setImage:image];
            [[wfStatusItem button] setImagePosition:NSImageOnly];
        }
        [[wfStatusItem button] setToolTip:@"XIASS Tools"];
        if (getenv("WF_TRAY_DEBUG") != NULL) {
            NSRect frame = [[wfStatusItem button] frame];
            NSWindow *buttonWindow = [[wfStatusItem button] window];
            NSRect windowFrame = [buttonWindow frame];
            fprintf(stderr, "[wf tray] item=%p visible=%d length=%.1f image=%p frame=%.1f,%.1f,%.1f,%.1f window=%p screenFrame=%.1f,%.1f,%.1f,%.1f\\n",
                    wfStatusItem,
                    [wfStatusItem isVisible],
                    [wfStatusItem length],
                    [[wfStatusItem button] image],
                    frame.origin.x,
                    frame.origin.y,
                    frame.size.width,
                    frame.size.height,
                    buttonWindow,
                    windowFrame.origin.x,
                    windowFrame.origin.y,
                    windowFrame.size.width,
                    windowFrame.size.height);
            fflush(stderr);
        }
    });
}

void wfTrayStop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (wfStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:wfStatusItem];
            wfStatusItem = nil;
        }
        if (wfTrayController != nil) {
            [[NSNotificationCenter defaultCenter] removeObserver:wfTrayController];
        }
        wfTrayController = nil;
    });
}

void wfTraySetDockVisible(int visible) {
    dispatch_async(dispatch_get_main_queue(), ^{
        // Closing the Wails window uses HideWindowOnClose. Keep the process a
        // regular macOS app in both states so its Dock tile remains available
        // for reopening and the standard Dock "Quit" command. The status-item
        // menu remains a second, independent way to show or exit the app.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        if (visible) {
            [NSApp activateIgnoringOtherApps:YES];
        }
    });
}

void wfTrayQuitMainLoop(void) {
    // Wails' close callback is intentionally used to hide a window. An
    // explicit menu-bar exit must instead stop the Cocoa loop on its owning
    // thread so the Go application can run OnShutdown and leave no proxy
    // listener behind.
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        [NSApp abortModal];
    });
}
