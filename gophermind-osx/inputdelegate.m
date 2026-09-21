// Return-to-send for the message field.
//
// This lives in its own .m rather than in chatinput.go's cgo preamble: a
// preamble is compiled into the object of every file in the package, so an
// @implementation there is emitted repeatedly and the link fails with
// "duplicate symbol _OBJC_CLASS_$_GMInputDelegate". A real translation unit
// is compiled once.
//
// Why a delegate at all: libui-ng exposes no key hook on uiMultilineEntry --
// only OnChanged, after the fact -- and no accelerator API, as ui/input.go's
// ShouldSend documents after checking ui.h. But the control is a real
// NSTextView inside an NSScrollView, reachable through uiControlHandle, and
// NSTextView asks its delegate what a command key means before acting on it.
// AppKit keeps doing the text editing; this only decides what Return does.

#import <AppKit/AppKit.h>
#include "_cgo_export.h"

@interface GMInputDelegate : NSObject <NSTextViewDelegate>
@property (nonatomic) long long handle;
@end

@implementation GMInputDelegate
- (BOOL)textView:(NSTextView *)tv doCommandBySelector:(SEL)sel {
	if (sel == @selector(insertNewline:)) {
		// Shift is read from the current event because doCommandBySelector
		// carries the selector and nothing else.
		NSEvent *e = [NSApp currentEvent];
		if (e && ([e modifierFlags] & NSEventModifierFlagShift)) {
			return NO; // Shift+Return: let AppKit insert the newline.
		}
		goInputEnterPressed(self.handle);
		return YES; // handled, so no newline is inserted
	}
	return NO;
}
@end

void installInputEnterHandler(uintptr_t control_handle, long long handle) {
	id view = (id)control_handle;
	NSTextView *tv = nil;
	if ([view isKindOfClass:[NSScrollView class]]) {
		tv = (NSTextView *)[(NSScrollView *)view documentView];
	} else if ([view isKindOfClass:[NSTextView class]]) {
		tv = (NSTextView *)view;
	}
	if (tv == nil) {
		return;
	}
	GMInputDelegate *d = [[GMInputDelegate alloc] init];
	d.handle = handle;
	[tv setDelegate:(id<NSTextViewDelegate>)d];
	// Retained for the life of the field, which is the life of the window.
	CFRetain((CFTypeRef)d);
}
