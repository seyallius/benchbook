// Package render. chart - inline SVG line charts generated in Go. No charting
// library and no client-side rendering: a chart is a string, so it works in a
// README, on GitHub Pages and in a printed PDF alike. Colours come from CSS custom
// properties with fallbacks, so the same markup follows the light, dark and neon
// themes.
package render

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Chart geometry in SVG user units. The viewBox does the scaling, so these are the
// only numbers the layout depends on.
const (
	// chartWidth is the logical width of the drawing area.
	chartWidth = 900.0
	// chartHeight is the logical height of the drawing area.
	chartHeight = 320.0
	// chartPaddingLeft leaves room for the y-axis labels.
	chartPaddingLeft = 64.0
	// chartPaddingRight keeps the last point away from the frame.
	chartPaddingRight = 24.0
	// chartPaddingTop keeps the topmost point inside the frame.
	chartPaddingTop = 24.0
	// chartPaddingBottom leaves room for the x-axis labels.
	chartPaddingBottom = 48.0
	// chartStrokeWidth is the width of a series line.
	chartStrokeWidth = 2.4
	// chartCurrentStrokeWidth is the width of the dotted segment to the working run.
	chartCurrentStrokeWidth = 2.0
	// chartDotRadius is the radius of a milestone marker.
	chartDotRadius = 3.0
	// chartCurrentDotRadius is the radius of the unpromoted working-run marker.
	chartCurrentDotRadius = 4.0
	// chartBreakWidth is the width of the hardware-change marker.
	chartBreakWidth = 2.0
	// chartLabelFontSize is the size of the axis labels.
	chartLabelFontSize = 12.0
	// chartLegendFontSize is the size of the legend text.
	chartLegendFontSize = 12.5
	// chartLegendRowHeight is the vertical step between legend rows.
	chartLegendRowHeight = 18.0
	// chartLegendColumns is how many legend entries share a row.
	chartLegendColumns = 2
	// chartLegendSwatchSize is the side of a legend colour square.
	chartLegendSwatchSize = 10.0
	// chartLegendSwatchOffset centres the swatch on the text baseline.
	chartLegendSwatchOffset = 3.0
	// chartLabelGap is the gap between a label and the axis it belongs to.
	chartLabelGap = 10.0
	// chartXLabelGap is the distance from the plot bottom to the x labels.
	chartXLabelGap = 20.0
	// chartGridLines is the target number of horizontal grid lines.
	chartGridLines = 5
	// chartPaddingFraction is the relative padding above and below the data range.
	chartPaddingFraction = 0.12
	// chartFlatPadding is used when every point has the same value.
	chartFlatPadding = 5.0
	// chartColorCount is the number of theme-provided series colours.
	chartColorCount = 8
	// chartColorVariableTemplate is the CSS custom property of series i.
	chartColorVariableTemplate = "var(--bb-chart-%d, currentColor)"
	// chartGridOpacity is the opacity of the grid lines.
	chartGridOpacity = "0.18"
	// chartAxisOpacity is the opacity of the frame and axis labels.
	chartAxisOpacity = "0.75"
	// chartLineClass is the CSS class of a promoted-milestone line.
	chartLineClass = "bb-chart-line"
	// chartCurrentClass is the CSS class of the segment leading to the unpromoted
	// current run, which is drawn dotted to look provisional.
	chartCurrentClass = "bb-chart-line bb-chart-current"
	// chartDotClass is the CSS class of a milestone point.
	chartDotClass = "bb-chart-dot"
	// chartCurrentDotClass is the CSS class of the unpromoted-run point.
	chartCurrentDotClass = "bb-chart-dot bb-chart-current-dot"
	// chartDashPattern draws the segment to the unpromoted run.
	chartDashPattern = "5,4"
	// chartBreakDashPattern draws a hardware-change marker.
	chartBreakDashPattern = "3,3"
	// chartMutedColor is the colour of every non-series element.
	chartMutedColor = "currentColor"
	// chartCoordinateDecimals bounds the printed precision of coordinates.
	chartCoordinateDecimals = 100
)

// ChartPoint is one measurement of one series.
type ChartPoint struct {
	// X is the index of the milestone slot, so a benchmark missing from one
	// milestone keeps its neighbours aligned instead of collapsing the axis.
	X int
	// Value is the metric value in base units.
	Value float64
	// Current marks the unpromoted working run.
	Current bool
}

// ChartSeries is one line of a chart, already normalised by the caller.
type ChartSeries struct {
	// Title is the legend label.
	Title string
	// Points are the measurements, in ascending X.
	Points []ChartPoint
}

// Chart is a ready-to-render line chart. The caller owns the x axis: it supplies one
// label per milestone slot and the positions of hardware changes.
type Chart struct {
	// Title is used for the accessible name of the SVG.
	Title string
	// XLabels is one label per x slot ("m001", "m002", "current").
	XLabels []string
	// Series are the lines to draw.
	Series []ChartSeries
	// YLabel is printed above the y axis, e.g. "index, m001 = 100".
	YLabel string
	// Breaks are the x indices preceded by a hardware change.
	Breaks []int
}

// ----------------------------------------- Public API ----------------------------------------- //

// LineChart renders the chart as inline SVG. Series without at least two usable
// points are dropped, and a chart without any series renders as an empty string so
// a caller can omit the section entirely rather than print an empty frame.
func LineChart(chart Chart) string {
	prepared := prepareSeries(chart.Series)
	if len(prepared) == 0 {
		return ""
	}
	scale := newChartScale(chart.XLabels, prepared)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(
		`<svg class="bb-chart" viewBox="0 0 %s %s" role="img" aria-label="%s" xmlns="http://www.w3.org/2000/svg">`+"\n",
		formatFloat(chartWidth), formatFloat(chartHeight),
		escapeAttribute(chart.Title+" — y axis is an index, the first milestone is 100")))
	builder.WriteString(fmt.Sprintf("<title>%s</title>\n", escapeTextXML(chart.Title)))
	builder.WriteString(chartGrid(scale))
	builder.WriteString(chartBreaks(scale, chart.Breaks))
	for index, series := range prepared {
		builder.WriteString(chartSeriesMarkup(scale, series, index))
	}
	builder.WriteString(chartAxis(scale, chart))
	builder.WriteString(chartLegend(scale, prepared))
	builder.WriteString("</svg>\n")
	return builder.String()
}

// ---------------------------------------------- Types ----------------------------------------- //

// chartScale maps milestone slots to pixels.
type chartScale struct {
	// width is the plot width in user units.
	width float64
	// height is the plot height in user units.
	height float64
	// slots is the number of x positions.
	slots int
	// minY and maxY bound the normalised values.
	minY float64
	// maxY bounds the upper normalised value.
	maxY float64
}

// preparedSeries is a series with usable points only.
type preparedSeries struct {
	// Title is the legend label.
	Title string
	// Points are the usable measurements.
	Points []ChartPoint
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// prepareSeries drops series without a baseline and sorts their points, so the
// renderer never depends on the caller's ordering.
func prepareSeries(series []ChartSeries) []preparedSeries {
	out := make([]preparedSeries, 0, len(series))
	for _, one := range series {
		points := make([]ChartPoint, 0, len(one.Points))
		for _, point := range one.Points {
			if point.Value > 0 {
				points = append(points, point)
			}
		}
		sort.SliceStable(points, func(i, j int) bool { return points[i].X < points[j].X })
		if len(points) < 2 {
			continue
		}
		out = append(out, preparedSeries{Title: one.Title, Points: normalise(points)})
	}
	return out
}

// normalise indexes a series against its first point (first point = 100).
func normalise(points []ChartPoint) []ChartPoint {
	baseline := points[0].Value
	out := make([]ChartPoint, 0, len(points))
	for _, point := range points {
		out = append(out, ChartPoint{
			X:       point.X,
			Value:   point.Value / baseline * 100,
			Current: point.Current,
		})
	}
	return out
}

// newChartScale builds the pixel mapping for a set of prepared series.
func newChartScale(labels []string, series []preparedSeries) chartScale {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, one := range series {
		for _, point := range one.Points {
			minY = math.Min(minY, point.Value)
			maxY = math.Max(maxY, point.Value)
		}
	}
	if math.IsInf(minY, 1) || math.IsInf(maxY, -1) {
		minY, maxY = 100, 100
	}
	padding := (maxY - minY) * chartPaddingFraction
	if padding <= 0 {
		padding = chartFlatPadding
	}
	slots := len(labels)
	if slots < 2 {
		slots = 2
	}
	return chartScale{
		width:  chartWidth - chartPaddingLeft - chartPaddingRight,
		height: chartHeight - chartPaddingTop - chartPaddingBottom,
		slots:  slots,
		minY:   minY - padding,
		maxY:   maxY + padding,
	}
}

// x maps a milestone slot to a pixel coordinate.
func (s chartScale) x(slot int) float64 {
	if s.slots < 2 {
		return chartPaddingLeft
	}
	return chartPaddingLeft + float64(slot)/float64(s.slots-1)*s.width
}

// slotStep is the horizontal distance between two milestone slots.
func (s chartScale) slotStep() float64 { return s.x(1) - s.x(0) }

// y maps a normalised value to a pixel coordinate.
func (s chartScale) y(value float64) float64 {
	if s.maxY == s.minY {
		return chartPaddingTop + s.height/2
	}
	return chartPaddingTop + (1-(value-s.minY)/(s.maxY-s.minY))*s.height
}

// chartGrid renders the frame, the horizontal grid lines and their labels.
func chartGrid(scale chartScale) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(
		`<rect class="bb-chart-frame" x="%s" y="%s" width="%s" height="%s" fill="none" stroke="%s" stroke-opacity="%s"/>`+"\n",
		formatFloat(chartPaddingLeft), formatFloat(chartPaddingTop),
		formatFloat(scale.width), formatFloat(scale.height), chartMutedColor, chartAxisOpacity))
	for _, value := range gridValues(scale) {
		y := scale.y(value)
		builder.WriteString(fmt.Sprintf(
			`<line class="bb-chart-grid" x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-opacity="%s"/>`+"\n",
			formatFloat(chartPaddingLeft), formatFloat(y),
			formatFloat(chartPaddingLeft+scale.width), formatFloat(y), chartMutedColor, chartGridOpacity))
		builder.WriteString(fmt.Sprintf(
			`<text class="bb-chart-axis-label" x="%s" y="%s" font-size="%s" fill="%s" text-anchor="end" dominant-baseline="middle">%s</text>`+"\n",
			formatFloat(chartPaddingLeft-chartLabelGap), formatFloat(y), formatFloat(chartLabelFontSize),
			chartMutedColor, formatFloat(math.Round(value))))
	}
	return builder.String()
}

// gridValues returns the values at which grid lines are drawn.
func gridValues(scale chartScale) []float64 {
	step := niceStep(scale.maxY - scale.minY)
	values := []float64{}
	for value := math.Ceil(scale.minY/step) * step; value <= scale.maxY; value += step {
		values = append(values, value)
	}
	return values
}

// niceStep picks a round grid step near span/chartGridLines.
func niceStep(span float64) float64 {
	if span <= 0 {
		return 10
	}
	rough := span / chartGridLines
	magnitude := math.Pow(10, math.Floor(math.Log10(rough)))
	for _, factor := range []float64{1, 2, 2.5, 5, 10} {
		if step := factor * magnitude; step >= rough {
			return step
		}
	}
	return 10 * magnitude
}

// chartBreaks renders a vertical marker before every hardware change.
func chartBreaks(scale chartScale, breaks []int) string {
	if len(breaks) == 0 {
		return ""
	}
	ordered := append([]int{}, breaks...)
	sort.Ints(ordered)
	var builder strings.Builder
	for _, slot := range ordered {
		position := scale.x(slot) - scale.slotStep()/2
		builder.WriteString(fmt.Sprintf(
			`<line class="bb-chart-break" x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-opacity="%s" stroke-width="%s" stroke-dasharray="%s"/>`+"\n",
			formatFloat(position), formatFloat(chartPaddingTop),
			formatFloat(position), formatFloat(chartPaddingTop+scale.height),
			chartMutedColor, chartAxisOpacity, formatFloat(chartBreakWidth), chartBreakDashPattern))
	}
	return builder.String()
}

// chartSeriesMarkup renders one series: its lines, the dotted segment to the
// working run, and its markers.
func chartSeriesMarkup(scale chartScale, series preparedSeries, index int) string {
	color := fmt.Sprintf(chartColorVariableTemplate, index%chartColorCount+1)
	var builder strings.Builder
	solid := []ChartPoint{}
	for _, point := range series.Points {
		if point.Current && len(solid) > 0 {
			last := solid[len(solid)-1]
			builder.WriteString(renderPolyline(scale, solid, color, "", chartStrokeWidth, chartLineClass))
			builder.WriteString(renderPolyline(scale, []ChartPoint{last, point}, color, chartDashPattern, chartCurrentStrokeWidth, chartCurrentClass))
			solid = []ChartPoint{point}
			continue
		}
		solid = append(solid, point)
	}
	if len(solid) > 1 {
		builder.WriteString(renderPolyline(scale, solid, color, "", chartStrokeWidth, chartLineClass))
	}
	for _, point := range series.Points {
		radius := chartDotRadius
		class := chartDotClass
		if point.Current {
			radius = chartCurrentDotRadius
			class = chartCurrentDotClass
		}
		builder.WriteString(fmt.Sprintf(
			`<circle class="%s" cx="%s" cy="%s" r="%s" fill="%s"/>`+"\n",
			class, formatFloat(scale.x(point.X)), formatFloat(scale.y(point.Value)), formatFloat(radius), color))
	}
	return builder.String()
}

// renderPolyline renders one polyline element.
func renderPolyline(scale chartScale, points []ChartPoint, color, dash string, width float64, class string) string {
	if len(points) < 2 {
		return ""
	}
	coords := make([]string, 0, len(points))
	for _, point := range points {
		coords = append(coords, fmt.Sprintf("%s,%s", formatFloat(scale.x(point.X)), formatFloat(scale.y(point.Value))))
	}
	dashAttribute := ""
	if dash != "" {
		dashAttribute = fmt.Sprintf(` stroke-dasharray="%s"`, dash)
	}
	return fmt.Sprintf(
		`<polyline class="%s" points="%s" fill="none" stroke="%s" stroke-width="%s" stroke-linejoin="round" stroke-linecap="round"%s/>`+"\n",
		class, strings.Join(coords, " "), color, formatFloat(width), dashAttribute)
}

// chartAxis renders the x labels and the y axis title.
func chartAxis(scale chartScale, chart Chart) string {
	var builder strings.Builder
	for slot, label := range chart.XLabels {
		builder.WriteString(fmt.Sprintf(
			`<text class="bb-chart-axis-label" x="%s" y="%s" font-size="%s" fill="%s" text-anchor="middle">%s</text>`+"\n",
			formatFloat(scale.x(slot)), formatFloat(chartHeight-chartPaddingBottom+chartXLabelGap),
			formatFloat(chartLabelFontSize), chartMutedColor, escapeTextXML(label)))
	}
	if chart.YLabel != "" {
		builder.WriteString(fmt.Sprintf(
			`<text class="bb-chart-axis-title" x="%s" y="%s" font-size="%s" fill="%s" text-anchor="start">%s</text>`+"\n",
			formatFloat(chartPaddingLeft), formatFloat(chartPaddingTop-chartLabelGap),
			formatFloat(chartLabelFontSize), chartMutedColor, escapeTextXML(chart.YLabel)))
	}
	return builder.String()
}

// chartLegend renders the series legend below the axis.
func chartLegend(scale chartScale, series []preparedSeries) string {
	var builder strings.Builder
	columnWidth := scale.width / float64(chartLegendColumns)
	for index, one := range series {
		column := index % chartLegendColumns
		row := index / chartLegendColumns
		x := chartPaddingLeft + float64(column)*columnWidth
		y := chartHeight - chartPaddingBottom + chartXLabelGap + chartLegendRowHeight*float64(row+1)
		builder.WriteString(fmt.Sprintf(
			`<rect class="bb-chart-swatch" x="%s" y="%s" width="%s" height="%s" rx="1" fill="%s"/>`+"\n",
			formatFloat(x), formatFloat(y-chartLegendSwatchSize/2-chartLegendSwatchOffset),
			formatFloat(chartLegendSwatchSize), formatFloat(chartLegendSwatchSize),
			fmt.Sprintf(chartColorVariableTemplate, index%chartColorCount+1)))
		builder.WriteString(fmt.Sprintf(
			`<text class="bb-chart-legend-label" x="%s" y="%s" font-size="%s" fill="%s">%s</text>`+"\n",
			formatFloat(x+chartLegendSwatchSize+chartLabelGap), formatFloat(y),
			formatFloat(chartLegendFontSize), chartMutedColor, escapeTextXML(one.Title)))
	}
	return builder.String()
}

// escapeTextXML escapes the characters that would break XML content.
func escapeTextXML(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

// escapeAttribute escapes the characters that would break an XML attribute.
func escapeAttribute(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(value)
}

// formatFloat renders a coordinate with bounded decimals, so the SVG is stable
// between runs and readable in a diff.
func formatFloat(value float64) string {
	rounded := math.Round(value*chartCoordinateDecimals) / chartCoordinateDecimals
	if rounded == math.Trunc(rounded) {
		return strconv.FormatFloat(rounded, 'f', 0, 64)
	}
	return strconv.FormatFloat(rounded, 'f', 2, 64)
}
