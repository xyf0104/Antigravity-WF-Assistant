#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

@interface WFTrayController : NSObject
- (void)showMainWindow:(id)sender;
- (void)quitApplication:(id)sender;
- (void)applicationDidHide:(NSNotification *)notification;
@end

@implementation WFTrayController

- (void)showMainWindow:(id)sender {
    wfTrayShowWindow();
}

- (void)quitApplication:(id)sender {
    wfTrayQuitApplication();
}

- (void)applicationDidHide:(NSNotification *)notification {
    // Wails' HideWindowOnClose hides the complete app on macOS. Once hidden,
    // turn it into a true menu-bar background app so it no longer owns a Dock
    // tile. The status item remains available for reopening it.
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}

@end

static NSStatusItem *wfStatusItem = nil;
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
            wfStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
            wfTrayController = [[WFTrayController alloc] init];
            [[NSNotificationCenter defaultCenter] addObserver:wfTrayController
                                                     selector:@selector(applicationDidHide:)
                                                         name:NSApplicationDidHideNotification
                                                       object:NSApp];

            NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Antigravity WF助手"];
            NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:@"打开主界面"
                                                               action:@selector(showMainWindow:)
                                                        keyEquivalent:@""];
            [showItem setTarget:wfTrayController];
            [menu addItem:showItem];
            [menu addItem:[NSMenuItem separatorItem]];

            NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"退出 Antigravity WF助手"
                                                               action:@selector(quitApplication:)
                                                        keyEquivalent:@""];
            [quitItem setTarget:wfTrayController];
            [menu addItem:quitItem];
            [wfStatusItem setMenu:menu];
        }

        NSImage *image = [[NSImage alloc] initWithData:iconData];
        if (image != nil) {
            [image setSize:NSMakeSize(18.0, 18.0)];
            [image setTemplate:NO];
            [[wfStatusItem button] setImage:image];
            [[wfStatusItem button] setImagePosition:NSImageOnly];
        }
        [[wfStatusItem button] setToolTip:@"Antigravity WF助手"];
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
        if (visible) {
            [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
            [NSApp activateIgnoringOtherApps:YES];
        } else {
            [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
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
