#import <Cocoa/Cocoa.h>
#import <stdlib.h>
#import "menubar.h"
#import "_cgo_export.h"

@interface FGController : NSObject <NSApplicationDelegate, NSMenuDelegate>
@property (strong) NSStatusItem *statusItem;
@property (strong) NSMenuItem *proxyItem;
@property (strong) NSMenuItem *interceptItem;
@property (strong) NSMenuItem *openItem;
@property (strong) NSMenuItem *systemProxyItem;
@property (strong) NSMenuItem *certItem;
@property (strong) NSMenuItem *autostartItem;
@end

@implementation FGController

- (void)applicationDidFinishLaunching:(NSNotification *)note {
    self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    NSImage *image = [NSImage imageWithSystemSymbolName:@"network" accessibilityDescription:@"FastGithub"];
    image.template = YES;
    self.statusItem.button.image = image;

    NSMenu *menu = [[NSMenu alloc] init];
    menu.delegate = self;

    self.proxyItem = [[NSMenuItem alloc] initWithTitle:@"代理：启动中…" action:nil keyEquivalent:@""];
    self.proxyItem.enabled = NO;
    [menu addItem:self.proxyItem];

    self.interceptItem = [[NSMenuItem alloc] initWithTitle:@"透明拦截：macOS 不支持" action:nil keyEquivalent:@""];
    self.interceptItem.enabled = NO;
    [menu addItem:self.interceptItem];
    [menu addItem:[NSMenuItem separatorItem]];

    self.openItem = [[NSMenuItem alloc] initWithTitle:@"打开 Web 界面" action:@selector(openUI:) keyEquivalent:@"o"];
    self.openItem.target = self;
    [menu addItem:self.openItem];

    self.systemProxyItem = [[NSMenuItem alloc] initWithTitle:@"系统代理：检测中…" action:@selector(toggleProxy:) keyEquivalent:@""];
    self.systemProxyItem.target = self;
    [menu addItem:self.systemProxyItem];
    [menu addItem:[NSMenuItem separatorItem]];

    self.certItem = [[NSMenuItem alloc] initWithTitle:@"安装 CA" action:@selector(toggleCert:) keyEquivalent:@""];
    self.certItem.target = self;
    [menu addItem:self.certItem];

    self.autostartItem = [[NSMenuItem alloc] initWithTitle:@"开机启动" action:@selector(toggleAutostart:) keyEquivalent:@""];
    self.autostartItem.target = self;
    [menu addItem:self.autostartItem];
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"退出 FastGithub" action:@selector(terminate:) keyEquivalent:@"q"];
    quit.target = NSApp;
    [menu addItem:quit];

    self.statusItem.menu = menu;
    [self refresh];
}

- (void)menuWillOpen:(NSMenu *)menu {
    [self refresh];
}

- (void)refresh {
    char *raw = fg_state_json();
    NSString *json = raw ? @(raw) : @"{}";
    free(raw);
    NSData *data = [json dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *state = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] ?: @{};

    self.proxyItem.title = state[@"proxyTitle"] ?: @"代理：未运行";
    self.systemProxyItem.title = state[@"systemProxyTitle"] ?: @"系统代理：不可用";
    NSString *sp = state[@"systemProxy"] ?: @"";
    self.systemProxyItem.enabled = [sp isEqualToString:@"enabled"] || [sp isEqualToString:@"disabled"];
    self.certItem.title = state[@"certTitle"] ?: @"安装 CA";
    self.autostartItem.state = [state[@"autostart"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
}

- (void)openUI:(id)sender {
    if (fg_open_ui() != 0) {
        [self showError];
    }
}

- (void)toggleProxy:(id)sender {
    if (fg_toggle_proxy() != 0) {
        [self showError];
    }
    [self refresh];
}

- (void)toggleCert:(id)sender {
    if (fg_toggle_cert() != 0) {
        [self showError];
    }
    [self refresh];
}

- (void)toggleAutostart:(id)sender {
    if (fg_toggle_autostart() != 0) {
        [self showError];
    }
    [self refresh];
}

- (void)showError {
    char *raw = fg_last_error();
    NSString *text = raw ? @(raw) : @"未知错误";
    free(raw);
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"FastGithub";
    alert.informativeText = text;
    [alert runModal];
}

@end

void fg_run_menu(void) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
        FGController *controller = [[FGController alloc] init];
        app.delegate = controller;
        [app run];
    }
}
