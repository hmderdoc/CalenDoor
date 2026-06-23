// The phone-subscribe screen: a QR of the calendar's webcal:// feed plus
// step-by-step iOS and Android guidance. This is the bridge that turns a BBS
// calendar into native phone reminders - scan once, and every event (with its
// connection info) buzzes your phone at the right time.
package main

import (
	"bufio"
	"strings"
)

// webcalURL converts an http(s) ICS URL to webcal://, which iOS (and ICS-aware
// Android apps) treat as "subscribe to this calendar".
func webcalURL(u string) string {
	if strings.HasPrefix(u, "https://") {
		return "webcal://" + strings.TrimPrefix(u, "https://")
	}
	if strings.HasPrefix(u, "http://") {
		return "webcal://" + strings.TrimPrefix(u, "http://")
	}
	return u
}

func runQRSubscribe(w *bufio.Writer, in *input, cols, rows int, cal Calendar) {
	cls(w)
	at(w, 1, 1, sgr("1;30;46", center("Subscribe on your phone: "+cal.Name, cols)))

	link := webcalURL(cal.URL)
	lines, qw, ok := qrHalfBlock(link, 2)

	qrTop := 3
	textCol := 2
	if ok && qw+4 < cols {
		for i, ln := range lines {
			if qrTop+i > rows-2 {
				break
			}
			at(w, qrTop+i, 2, ln)
		}
		textCol = qw + 5 // guidance sits to the right of the code
	} else {
		at(w, qrTop, 2, sgr("0;37", "(screen too small to draw the code - use the link below)"))
	}

	// iOS / Android instructions (kept narrow so they fit beside the code).
	tw := cols - textCol - 1
	row := qrTop
	put := func(style, s string) {
		if row <= rows-2 {
			at(w, row, textCol, sgr(style, truncate(s, tw)))
		}
		row++
	}
	put("1;33", "ON iPHONE / iPAD")
	put("0;37", "1. Open the Camera app")
	put("0;37", "2. Point it at the code")
	put("0;37", "3. Tap the Calendar banner,")
	put("0;37", "   then Subscribe")
	row++
	put("1;32", "ON ANDROID")
	put("0;37", "Scan with your camera and")
	put("0;37", "open in your calendar app")
	put("0;37", "(e.g. ICSx5). Or in Google")
	put("0;37", "Calendar on the web: Other")
	put("0;37", "calendars > From URL.")
	row++
	put("1;36", "Why subscribe?")
	put("0;37", "Events show in YOUR timezone")
	put("0;37", "and your phone reminds you -")
	put("0;37", "with the join link in each one.")

	drawFooter(w, cols, rows, "ESC back")
	w.Flush()

	for {
		select {
		case <-in.quitCh:
			return
		case k := <-in.events:
			if k == keyEsc || k == keyBack || k == keyEnter || (k > 0 && toLower(rune(k)) == 'q') {
				return
			}
		}
	}
}
