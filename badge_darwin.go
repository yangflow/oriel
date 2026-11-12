package main

import (
	"log/slog"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// requestDockBadge asks only for badge authorization. MyGo loads UserNotifications with its native backend;
// currentNotificationCenter must not be called by a bare command-line executable.
func requestDockBadge(done func(bool)) {
	class := objc.GetClass("UNUserNotificationCenter")
	if !mygo.App.IsPackaged() || class == 0 {
		slog.Warn("Dock badge permission needs a macOS app bundle")
		done(false)
		return
	}
	center := objc.ID(class).Send(objc.RegisterName("currentNotificationCenter"))
	block := objc.NewBlock(func(_ objc.Block, granted bool, err objc.ID) {
		if !granted || err != 0 {
			slog.Warn("Dock badges are not allowed; check notification settings")
			done(false)
			return
		}
		settingsBlock := objc.NewBlock(func(_ objc.Block, settings objc.ID) {
			// UNNotificationSettingEnabled is 2. General authorization alone does not imply badges are enabled.
			allowed := settings.Send(objc.RegisterName("badgeSetting")) == 2
			if !allowed {
				slog.Warn("Dock badges are disabled in notification settings")
			}
			done(allowed)
		})
		center.Send(objc.RegisterName("getNotificationSettingsWithCompletionHandler:"), settingsBlock)
		settingsBlock.Release()
	})
	// UNAuthorizationOptionBadge is 1 << 0; do not request sound or alerts.
	center.Send(objc.RegisterName("requestAuthorizationWithOptions:completionHandler:"), uintptr(1), block)
	block.Release() // the system retains the completion block until it answers
}
