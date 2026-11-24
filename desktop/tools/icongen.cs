// Turns the flat white background of the project icon into transparency and
// writes the PNG and multi-size ICO used by the Windows build.
// Compile and run through desktop/tools/make-icon.ps1.
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.IO;

public static class IconGen
{
    // RunFlat keeps the picture exactly as drawn: only scaling, no background removal.
    public static string RunFlat(string source, string pngPath, string icoPath)
    {
        using (var bmp = new Bitmap(source))
        {
            int side = Math.Max(bmp.Width, bmp.Height);
            var canvas = new Bitmap(side, side, PixelFormat.Format32bppArgb);
            using (var g = Graphics.FromImage(canvas))
            {
                // A transparent source keeps its alpha; an opaque one stays opaque.
                g.Clear(Color.Transparent);
                g.DrawImage(bmp, (side - bmp.Width) / 2, (side - bmp.Height) / 2);
            }
            Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(pngPath)));
            var master = Scale(canvas, 256);
            master.Save(pngPath, ImageFormat.Png);
            master.Dispose();
            int[] sizes = { 16, 20, 24, 32, 40, 48, 64, 128, 256 };
            var images = new List<byte[]>();
            foreach (int s in sizes) images.Add(Encode(Scale(canvas, s), s));
            using (var fs = File.Create(icoPath)) WriteIco(fs, sizes, images);
            canvas.Dispose();
            return string.Format("{0} and {1} from {2}", pngPath, icoPath, source);
        }
    }

    static byte[] LoadArgb(string path, out int w, out int h)
    {
        using (var bmp = new Bitmap(path))
        {
            w = bmp.Width;
            h = bmp.Height;
            var rect = new Rectangle(0, 0, w, h);
            var copy = new Bitmap(w, h, PixelFormat.Format32bppArgb);
            using (var g = Graphics.FromImage(copy))
            {
                g.CompositingMode = CompositingMode.SourceCopy;
                g.DrawImage(bmp, rect);
            }
            var data = copy.LockBits(rect, ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
            var bytes = new byte[data.Stride * h];
            System.Runtime.InteropServices.Marshal.Copy(data.Scan0, bytes, 0, bytes.Length);
            copy.UnlockBits(data);
            copy.Dispose();
            return bytes;
        }
    }

    public static string Run(string source, string pngPath, string icoPath)
    {
        int w, h;
        var argb = LoadArgb(source, out w, out h);
        int stride = w * 4;

        // Flood fill the near-white background inwards from the border.
        var outside = new bool[w * h];
        var stack = new Stack<int>();
        Func<int, bool> white = i =>
        {
            int p = i * 4;
            return argb[p] >= 244 && argb[p + 1] >= 244 && argb[p + 2] >= 244;
        };
        for (int x = 0; x < w; x++)
        {
            stack.Push(x);
            stack.Push((h - 1) * w + x);
        }
        for (int y = 0; y < h; y++)
        {
            stack.Push(y * w);
            stack.Push(y * w + w - 1);
        }
        while (stack.Count > 0)
        {
            int i = stack.Pop();
            if (outside[i] || !white(i)) continue;
            outside[i] = true;
            int x = i % w, y = i / w;
            if (x > 0) stack.Push(i - 1);
            if (x < w - 1) stack.Push(i + 1);
            if (y > 0) stack.Push(i - w);
            if (y < h - 1) stack.Push(i + w);
        }

        const double dark = 28.0; // the logo's near-black ink
        // How far each pixel sits from the removed background, in 4-neighbour steps.
        const int far = 1000;
        var distance = new int[w * h];
        for (int i = 0; i < w * h; i++) distance[i] = outside[i] ? 0 : far;
        for (int pass = 0; pass < 3; pass++)
            for (int y = 0; y < h; y++)
                for (int x = 0; x < w; x++)
                {
                    int i = y * w + x;
                    if (distance[i] == 0) continue;
                    int best = distance[i];
                    if (x > 0 && distance[i - 1] + 1 < best) best = distance[i - 1] + 1;
                    if (x < w - 1 && distance[i + 1] + 1 < best) best = distance[i + 1] + 1;
                    if (y > 0 && distance[i - w] + 1 < best) best = distance[i - w] + 1;
                    if (y < h - 1 && distance[i + w] + 1 < best) best = distance[i + w] + 1;
                    distance[i] = best;
                }

        for (int i = 0; i < w * h; i++)
        {
            int p = i * 4;
            if (outside[i])
            {
                // Bleed the ink outwards so rescaling cannot pull white into the edge.
                argb[p] = 38;
                argb[p + 1] = 28;
                argb[p + 2] = 24;
                argb[p + 3] = 0;
                continue;
            }
            if (distance[i] > 2) continue; // interior: opaque, untouched
            double lum = ((255.0 - argb[p]) + (255.0 - argb[p + 1]) + (255.0 - argb[p + 2])) / 3.0;
            double a = Math.Min(1.0, Math.Max(0.0, lum / (255.0 - dark)));
            if (a >= 0.97) continue;
            if (a <= 0.02)
            {
                argb[p] = 38;
                argb[p + 1] = 28;
                argb[p + 2] = 24;
                argb[p + 3] = 0;
                continue;
            }
            argb[p] = Clamp((argb[p] - (1.0 - a) * 255.0) / a);
            argb[p + 1] = Clamp((argb[p + 1] - (1.0 - a) * 255.0) / a);
            argb[p + 2] = Clamp((argb[p + 2] - (1.0 - a) * 255.0) / a);
            argb[p + 3] = (byte)Math.Round(a * 255.0);
        }

        int minX = w, minY = h, maxX = -1, maxY = -1;
        for (int y = 0; y < h; y++)
            for (int x = 0; x < w; x++)
                if (argb[(y * stride) + x * 4 + 3] > 0)
                {
                    if (x < minX) minX = x;
                    if (y < minY) minY = y;
                    if (x > maxX) maxX = x;
                    if (y > maxY) maxY = y;
                }
        if (maxX < 0) throw new InvalidOperationException("icon is empty after background removal");

        int cw = maxX - minX + 1, ch = maxY - minY + 1;
        int side = (int)Math.Round(Math.Max(cw, ch) * 1.03);
        var square = new Bitmap(side, side, PixelFormat.Format32bppArgb);
        var pixels = square.LockBits(new Rectangle(0, 0, side, side), ImageLockMode.WriteOnly, PixelFormat.Format32bppArgb);
        var outBytes = new byte[pixels.Stride * side];
        int ox = (side - cw) / 2, oy = (side - ch) / 2;
        for (int y = 0; y < ch; y++)
            Array.Copy(argb, (minY + y) * stride + minX * 4, outBytes, (oy + y) * pixels.Stride + ox * 4, cw * 4);
        System.Runtime.InteropServices.Marshal.Copy(outBytes, 0, pixels.Scan0, outBytes.Length);
        square.UnlockBits(pixels);

        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(pngPath)));
        var master = Scale(square, 256);
        master.Save(pngPath, ImageFormat.Png);

        int[] sizes = { 16, 20, 24, 32, 40, 48, 64, 128, 256 };
        var images = new List<byte[]>();
        foreach (int s in sizes) images.Add(Encode(Scale(square, s), s));
        using (var fs = File.Create(icoPath)) WriteIco(fs, sizes, images);
        square.Dispose();
        master.Dispose();
        return string.Format("{0} ({1}x{2} ink {3}x{4}), {5}", pngPath, w, h, cw, ch, icoPath);
    }

    static byte Clamp(double v)
    {
        if (v < 0) return 0;
        if (v > 255) return 255;
        return (byte)Math.Round(v);
    }

    static Bitmap Scale(Bitmap source, int size)
    {
        var target = new Bitmap(size, size, PixelFormat.Format32bppArgb);
        using (var g = Graphics.FromImage(target))
        using (var attributes = new ImageAttributes())
        {
            attributes.SetWrapMode(WrapMode.TileFlipXY);
            g.CompositingMode = CompositingMode.SourceCopy;
            g.CompositingQuality = CompositingQuality.HighQuality;
            g.InterpolationMode = InterpolationMode.HighQualityBicubic;
            g.PixelOffsetMode = PixelOffsetMode.HighQuality;
            g.DrawImage(source, new Rectangle(0, 0, size, size), 0, 0, source.Width, source.Height, GraphicsUnit.Pixel, attributes);
        }
        return target;
    }

    static byte[] Encode(Bitmap image, int size)
    {
        using (image)
        using (var ms = new MemoryStream())
        {
            if (size >= 128)
            {
                image.Save(ms, ImageFormat.Png);
                return ms.ToArray();
            }
            int maskStride = ((size + 31) / 32) * 4;
            var header = new byte[40 + size * size * 4 + maskStride * size];
            BitConverter.GetBytes(40).CopyTo(header, 0);
            BitConverter.GetBytes(size).CopyTo(header, 4);
            BitConverter.GetBytes(size * 2).CopyTo(header, 8);
            BitConverter.GetBytes((short)1).CopyTo(header, 12);
            BitConverter.GetBytes((short)32).CopyTo(header, 14);
            var pixels = new byte[size * size * 4];
            var data = image.LockBits(new Rectangle(0, 0, size, size), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
            for (int y = 0; y < size; y++)
                System.Runtime.InteropServices.Marshal.Copy(data.Scan0 + y * data.Stride, pixels, y * size * 4, size * 4);
            image.UnlockBits(data);
            int offset = 40;
            for (int y = size - 1; y >= 0; y--)
            {
                Buffer.BlockCopy(pixels, y * size * 4, header, offset, size * 4);
                offset += size * 4;
            }
            for (int y = 0; y < size; y++)
                for (int x = 0; x < size; x++)
                    if (pixels[(y * size + x) * 4 + 3] == 0)
                        header[offset + (size - 1 - y) * maskStride + x / 8] |= (byte)(0x80 >> (x % 8));
            return header;
        }
    }

    static void WriteIco(Stream stream, int[] sizes, List<byte[]> images)
    {
        var w = new BinaryWriter(stream);
        w.Write((short)0);
        w.Write((short)1);
        w.Write((short)sizes.Length);
        int offset = 6 + 16 * sizes.Length;
        for (int i = 0; i < sizes.Length; i++)
        {
            w.Write((byte)(sizes[i] >= 256 ? 0 : sizes[i]));
            w.Write((byte)(sizes[i] >= 256 ? 0 : sizes[i]));
            w.Write((byte)0);
            w.Write((byte)0);
            w.Write((short)1);
            w.Write((short)32);
            w.Write(images[i].Length);
            w.Write(offset);
            offset += images[i].Length;
        }
        foreach (var image in images) w.Write(image);
        w.Flush();
    }
}
