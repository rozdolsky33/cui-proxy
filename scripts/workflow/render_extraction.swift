// Reference macOS renderer. Input and output remain in a private fixture directory.
import AppKit
import CoreText

struct ImageRecipe: Decodable {
    let id: String
    let format: String
    let width: Int
    let height: Int
    let expected: String
}
struct PDFRecipe: Decodable {
    let id: String
    let kind: String
    let pages: [String]
}
struct Recipes: Decodable {
    let images: [ImageRecipe]
    let pdfs: [PDFRecipe]
}
let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let recipes = try JSONDecoder().decode(Recipes.self, from: Data(contentsOf: root.appendingPathComponent("recipes.json")))
let font = CTFontCreateWithName("Menlo-Regular" as CFString, 28, nil)
guard CTFontCopyPostScriptName(font) as String == "Menlo-Regular" else {
    fatalError("reference_font_unavailable")
}
try JSONSerialization.data(withJSONObject: ["postscript_name": CTFontCopyPostScriptName(font) as String])
    .write(to: root.appendingPathComponent("font.json"))
func draw(_ text: String, _ context: CGContext, _ height: Int) {
    context.setFillColor(CGColor(gray: 1, alpha: 1))
    context.fill(CGRect(x: 0, y: 0, width: context.boundingBoxOfClipPath.width, height: CGFloat(height)))
    for (index, line) in text.components(separatedBy: "\n").enumerated() {
        let attrs: [NSAttributedString.Key: Any] = [NSAttributedString.Key(kCTFontAttributeName as String): font,
            NSAttributedString.Key(kCTForegroundColorAttributeName as String): CGColor(gray: 0, alpha: 1)]
        let ctline = CTLineCreateWithAttributedString(NSAttributedString(string: line, attributes: attrs))
        context.textPosition = CGPoint(x: 48, y: height - 72 - index * 48)
        CTLineDraw(ctline, context)
    }
}
func raster(_ text: String, _ width: Int, _ height: Int) -> CGImage {
    let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8,
        bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    draw(text, context, height)
    return context.makeImage()!
}
for recipe in recipes.images {
    let rep = NSBitmapImageRep(cgImage: raster(recipe.expected, recipe.width, recipe.height))
    let type: NSBitmapImageRep.FileType = recipe.format == "png" ? .png : .jpeg
    let data = rep.representation(using: type, properties: [.compressionFactor: 0.9])!
    try data.write(to: root.appendingPathComponent(recipe.id + "." + recipe.format))
}
for recipe in recipes.pdfs {
    var box = CGRect(x: 0, y: 0, width: 1024, height: 768)
    let context = CGContext(root.appendingPathComponent(recipe.id + ".pdf") as CFURL, mediaBox: &box, nil)!
    for text in recipe.pages {
        context.beginPDFPage(nil)
        if recipe.kind == "scan" { context.draw(raster(text, 1024, 768), in: box) }
        else { draw(text, context, 768) }
        context.endPDFPage()
    }
    context.closePDF()
}
let fontURL = CTFontCopyAttribute(font, kCTFontURLAttribute) as! URL
try Data(contentsOf: fontURL).write(to: root.appendingPathComponent("font.bin"))
