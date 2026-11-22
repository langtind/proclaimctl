// Draws every page of a PDF into a 1920×1080 PNG, scaled to fit on white.
// Usage: osascript -l JavaScript raster.js <pdf> <out dir> <name>
// Prints the written paths, one per line, in page order.
ObjC.import('PDFKit');
ObjC.import('AppKit');

function run(argv) {
  const [pdfPath, outDir, name] = argv;
  const W = 1920, H = 1080;
  const doc = $.PDFDocument.alloc.initWithURL($.NSURL.fileURLWithPath(pdfPath));
  if (doc.isNil()) throw new Error('cannot read PDF ' + pdfPath);
  const paths = [];
  for (let i = 0; i < doc.pageCount; i++) {
    const page = doc.pageAtIndex(i);
    const box = page.boundsForBox($.kPDFDisplayBoxMediaBox);
    const rep = $.NSBitmapImageRep.alloc
      .initWithBitmapDataPlanesPixelsWidePixelsHighBitsPerSampleSamplesPerPixelHasAlphaIsPlanarColorSpaceNameBytesPerRowBitsPerPixel(
        null, W, H, 8, 4, true, false, $.NSDeviceRGBColorSpace, 0, 0);
    const ctx = $.NSGraphicsContext.graphicsContextWithBitmapImageRep(rep);
    $.NSGraphicsContext.saveGraphicsState;
    $.NSGraphicsContext.setCurrentContext(ctx);
    $.NSColor.whiteColor.setFill;
    $.NSRectFill($.NSMakeRect(0, 0, W, H));
    const scale = Math.min(W / box.size.width, H / box.size.height);
    const cg = ctx.CGContext;
    $.CGContextTranslateCTM(cg, (W - box.size.width * scale) / 2, (H - box.size.height * scale) / 2);
    $.CGContextScaleCTM(cg, scale, scale);
    page.drawWithBoxToContext($.kPDFDisplayBoxMediaBox, cg);
    $.NSGraphicsContext.restoreGraphicsState;
    const data = rep.representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $());
    const path = outDir + '/' + name + '-' + (i + 1) + '.png';
    if (!data.writeToFileAtomically(path, true)) throw new Error('cannot write ' + path);
    paths.push(path);
  }
  return paths.join('\n');
}
