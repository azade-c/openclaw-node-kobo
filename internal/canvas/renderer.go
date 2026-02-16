package canvas

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type HitTarget struct {
	Rect   image.Rectangle
	Action A2UIAction
}

type Renderer struct {
	Width      int
	Height     int
	Image      *image.Gray
	HitTargets []HitTarget
	face       font.Face
}

func NewRenderer(width, height int) *Renderer {
	img := image.NewGray(image.Rect(0, 0, width, height))
	return &Renderer{
		Width:  width,
		Height: height,
		Image:  img,
		face:   basicfont.Face7x13,
	}
}

func (r *Renderer) Clear() {
	draw.Draw(r.Image, r.Image.Bounds(), &image.Uniform{C: color.Gray{Y: 255}}, image.Point{}, draw.Src)
	r.HitTargets = nil
}

func (r *Renderer) Render(components []A2UIComponent) {
	r.Clear()
	for _, comp := range components {
		r.renderComponent(comp, 0, 0)
	}
}

func (r *Renderer) renderComponent(comp A2UIComponent, offsetX, offsetY int) {
	x := offsetX + comp.X
	y := offsetY + comp.Y
	width := comp.Width
	height := comp.Height
	if width <= 0 {
		width = r.Width - x
	}
	if height <= 0 {
		height = r.Height - y
	}
	rect := image.Rect(x, y, x+width, y+height)

	switch comp.Type {
	case "box", "card", "button":
		fill := uint8(230)
		if comp.Style != nil && comp.Style.FillGray != nil {
			fill = *comp.Style.FillGray
		}
		draw.Draw(r.Image, rect, &image.Uniform{C: color.Gray{Y: fill}}, image.Point{}, draw.Src)
		stroke := uint8(80)
		if comp.Style != nil && comp.Style.StrokeGray != nil {
			stroke = *comp.Style.StrokeGray
		}
		r.strokeRect(rect, stroke)
	case "text":
		textRect := rect
		textColor := color.Gray{Y: 20}
		r.drawText(comp.Text, textRect, textColor, comp.Align)
	}

	if comp.Action != nil && rect.Dx() > 0 && rect.Dy() > 0 {
		r.HitTargets = append(r.HitTargets, HitTarget{Rect: rect, Action: *comp.Action})
	}

	if len(comp.Children) == 0 {
		return
	}
	if comp.Type == "list" {
		cursorY := y + comp.Padding
		for _, child := range comp.Children {
			childY := child.Y
			if childY == 0 {
				childY = cursorY - y
			}
			child.X += comp.Padding
			child.Y = childY
			r.renderComponent(child, x, y)
			cursorY += child.Height + comp.Padding
		}
		return
	}
	for _, child := range comp.Children {
		r.renderComponent(child, x, y)
	}
}

func (r *Renderer) strokeRect(rect image.Rectangle, gray uint8) {
	strokeColor := color.Gray{Y: gray}
	for x := rect.Min.X; x < rect.Max.X; x++ {
		r.Image.SetGray(x, rect.Min.Y, strokeColor)
		r.Image.SetGray(x, rect.Max.Y-1, strokeColor)
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		r.Image.SetGray(rect.Min.X, y, strokeColor)
		r.Image.SetGray(rect.Max.X-1, y, strokeColor)
	}
}

func (r *Renderer) drawText(text string, rect image.Rectangle, col color.Gray, align string) {
	if text == "" {
		return
	}
	d := &font.Drawer{
		Dst:  r.Image,
		Src:  image.NewUniform(col),
		Face: r.face,
	}
	textWidth := d.MeasureString(text).Ceil()
	startX := rect.Min.X + 2
	if align == "center" {
		startX = rect.Min.X + (rect.Dx()-textWidth)/2
	} else if align == "right" {
		startX = rect.Max.X - textWidth - 2
	}
	startY := rect.Min.Y + r.face.Metrics().Ascent.Ceil() + 2
	d.Dot = fixed.P(startX, startY)
	d.DrawString(text)
}

func (r *Renderer) HitTest(x, y int) *A2UIAction {
	for i := range r.HitTargets {
		hit := r.HitTargets[i]
		if x >= hit.Rect.Min.X && x < hit.Rect.Max.X && y >= hit.Rect.Min.Y && y < hit.Rect.Max.Y {
			return &hit.Action
		}
	}
	return nil
}
