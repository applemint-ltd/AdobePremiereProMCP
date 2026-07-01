// Renders a single line (or, in vertical mode, a stack of characters) of text
// to a transparent PNG. Used by AddTextLayer as a fallback for Premiere Pro
// 2026, whose ExtendScript DOM cannot make a scripted Essential Graphics
// "Source Text" edit actually render (the data model updates, the compositor
// never repaints — confirmed live, exportAsMediaDirect included). Invoked as:
//
//   swift render_text_layer.swift <outputPath> <width> <height> <text> \
//       <fontName> <fontSize> <hexColor> <x> <y> [orientation]
//
// x/y are the text's normalized (0-1) anchor position, matching this
// project's convention elsewhere (position is normalized, not pixels).
// orientation is "horizontal" (default) or "vertical" (one character per
// line, centered — an approximation of Premiere's vertical text layout).
import AppKit
import Foundation

let args = CommandLine.arguments
guard args.count >= 9 else {
    FileHandle.standardError.write("Usage: render_text_layer <outputPath> <width> <height> <text> <fontName> <fontSize> <hexColor> <x> <y> [orientation]\n".data(using: .utf8)!)
    exit(1)
}

let outputPath = args[1]
let width = Int(args[2]) ?? 1920
let height = Int(args[3]) ?? 1080
let text = args[4]
let fontName = args[5]
let fontSize = CGFloat(Double(args[6]) ?? 72)
let hexColor = args[7]
let x = CGFloat(Double(args[8]) ?? 0.5)
let y = args.count > 9 ? CGFloat(Double(args[9]) ?? 0.5) : 0.5
let orientation = args.count > 10 ? args[10] : "horizontal"
let displayText = orientation == "vertical" ? text.map { String($0) }.joined(separator: "\n") : text

func colorFromHex(_ hex: String) -> NSColor {
    var hexSanitized = hex.trimmingCharacters(in: .whitespacesAndNewlines)
    hexSanitized = hexSanitized.replacingOccurrences(of: "#", with: "")
    var rgb: UInt64 = 0
    Scanner(string: hexSanitized).scanHexInt64(&rgb)
    let r = CGFloat((rgb & 0xFF0000) >> 16) / 255.0
    let g = CGFloat((rgb & 0x00FF00) >> 8) / 255.0
    let b = CGFloat(rgb & 0x0000FF) / 255.0
    return NSColor(red: r, green: g, blue: b, alpha: 1.0)
}

// NSImage+lockFocus renders at the screen's backing scale factor (2x on
// Retina), silently doubling the output dimensions. An NSBitmapImageRep
// created with explicit pixelsWide/pixelsHigh is scale-independent, so the
// output always matches the requested pixel dimensions exactly.
guard let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
) else {
    FileHandle.standardError.write("Failed to create bitmap\n".data(using: .utf8)!)
    exit(1)
}
guard let context = NSGraphicsContext(bitmapImageRep: rep) else {
    FileHandle.standardError.write("Failed to create graphics context\n".data(using: .utf8)!)
    exit(1)
}
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context

let font = NSFont(name: fontName, size: fontSize) ?? NSFont.boldSystemFont(ofSize: fontSize)
let color = colorFromHex(hexColor)

let shadow = NSShadow()
shadow.shadowColor = NSColor.black.withAlphaComponent(0.8)
shadow.shadowOffset = NSSize(width: 0, height: -2)
shadow.shadowBlurRadius = 6

let paragraphStyle = NSMutableParagraphStyle()
paragraphStyle.alignment = .center

// Negative strokeWidth fills AND strokes the glyph in one pass (positive
// strokeWidth draws an outline only, with no fill).
let attrs: [NSAttributedString.Key: Any] = [
    .font: font,
    .foregroundColor: color,
    .strokeColor: NSColor.black,
    .strokeWidth: -3.0,
    .shadow: shadow,
    .paragraphStyle: paragraphStyle
]

let attrStr = NSAttributedString(string: displayText, attributes: attrs)
let textSize = attrStr.size()

// x/y anchor the text's center; y is flipped since Premiere's convention
// (and this script's CLI contract) has y=0 at the top, but the bitmap
// context's origin is bottom-left.
let originX = (CGFloat(width) * x) - (textSize.width / 2)
let originY = (CGFloat(height) * (1 - y)) - (textSize.height / 2)

attrStr.draw(at: NSPoint(x: originX, y: originY))

NSGraphicsContext.restoreGraphicsState()

guard let pngData = rep.representation(using: .png, properties: [:]) else {
    FileHandle.standardError.write("Failed to render PNG\n".data(using: .utf8)!)
    exit(1)
}

do {
    try pngData.write(to: URL(fileURLWithPath: outputPath))
    print("OK")
} catch {
    FileHandle.standardError.write("Failed to write file: \(error)\n".data(using: .utf8)!)
    exit(1)
}
