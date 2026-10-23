// The picture as it is sent to the vision model: a raster data URL, no
// longer than MAX_SIDE on its long side. Vision APIs refuse SVG, cap the
// upload size, and scale anything bigger down to about this size anyway
// (Anthropic's guidance is 1568px), so sending more only costs time.

export interface Picture {
    dataUrl: string;
    name: string;
}

const MAX_SIDE = 1568;

const readAsDataUrl = (blob: Blob): Promise<string> => new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
});

const loadImage = (src: string): Promise<HTMLImageElement> => new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error('unreadable image'));
    image.src = src;
});

// Fits `width × height` inside MAX_SIDE, never enlarging.
export const fittedSize = (width: number, height: number, maxSide = MAX_SIDE): { width: number; height: number } => {
    const scale = Math.min(1, maxSide / Math.max(width, height));
    return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
};

export const preparePicture = async (source: File | string, name?: string): Promise<Picture> => {
    const src = typeof source === 'string' ? source : await readAsDataUrl(source);
    const label = name ?? (typeof source === 'string' ? 'image' : source.name);
    const image = await loadImage(src);
    const isRaster = /^data:image\/(png|jpe?g|webp|gif);/i.test(src);
    const longSide = Math.max(image.naturalWidth, image.naturalHeight);
    if (isRaster && longSide > 0 && longSide <= MAX_SIDE) return { dataUrl: src, name: label };

    // SVG (the Library's mock images, a pasted vector) and oversized pictures
    // are drawn once at the size that will be sent. JPEG on white: a photo's
    // size stays small, and no model reads transparency.
    const { width, height } = fittedSize(image.naturalWidth || MAX_SIDE, image.naturalHeight || MAX_SIDE);
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext('2d');
    if (!context) throw new Error('no canvas');
    context.fillStyle = '#fff';
    context.fillRect(0, 0, width, height);
    context.drawImage(image, 0, 0, width, height);
    return { dataUrl: canvas.toDataURL('image/jpeg', 0.9), name: label };
};
