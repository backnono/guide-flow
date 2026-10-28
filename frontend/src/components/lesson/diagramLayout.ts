import type { DiagramFigure } from '../../types/lesson'

export interface DiagramPointPosition {
  x: number
  y: number
}

export interface RenderedDiagramFigure {
  figure: DiagramFigure
  positions: Record<string, DiagramPointPosition>
  segments: string[]
  lines: Array<{ id: string; from: DiagramPointPosition; to: DiagramPointPosition }>
  curves: Array<{ id: string; from: DiagramPointPosition; to: DiagramPointPosition; radius: number; sweep: 'upper' | 'lower' }>
}

const VIEW_WIDTH = 280
const VIEW_HEIGHT = 205
const PADDING = 20

// 这是示意布局的默认矩形，不是题目坐标；真实长度关系由 constraints 决定。
const DEFAULT_RECTANGLE_ASPECT = 0.78

const fallbackPositions: Record<string, DiagramPointPosition> = {
  E: { x: 24, y: 170 },
  A: { x: 124, y: 170 },
  B: { x: 224, y: 170 },
  D: { x: 124, y: 38 },
  C: { x: 224, y: 38 },
}

function endpoints(line: string) {
  const points = line.replace(/[^A-Za-z]/g, '').toUpperCase().slice(0, 2).split('')
  return points.length === 2 ? points : null
}

function normalizedSegment(line: string) {
  const points = endpoints(line)
  return points ? points.slice().sort().join('') : null
}

function intersection(first: [DiagramPointPosition, DiagramPointPosition], second: [DiagramPointPosition, DiagramPointPosition]) {
  const [a, b] = first
  const [c, d] = second
  const denominator = (a.x - b.x) * (c.y - d.y) - (a.y - b.y) * (c.x - d.x)
  if (Math.abs(denominator) < 0.001) return null
  const firstCross = a.x * b.y - a.y * b.x
  const secondCross = c.x * d.y - c.y * d.x
  return {
    x: (firstCross * (c.x - d.x) - (a.x - b.x) * secondCross) / denominator,
    y: (firstCross * (c.y - d.y) - (a.y - b.y) * secondCross) / denominator,
  }
}

function rectangleAspect(figure: DiagramFigure) {
  const hint = figure.layoutHints?.aspectRatio
  if (hint && hint > 0) return Math.min(Math.max(hint, 0.35), 2.2)
  for (const constraint of figure.constraints ?? []) {
    if (constraint.type !== 'length_compare' || constraint.operator !== '<') continue
    const segments = constraint.segments?.map(normalizedSegment) ?? []
    if (segments.includes('AB') && segments.includes('AD')) return 0.72
  }
  return DEFAULT_RECTANGLE_ASPECT
}

function seedPositions(figure: DiagramFigure) {
  // 旧版矩形兜底坐标不能泄漏到三角形等其他图形，否则会制造“孤立点”错觉。
  const positions: Record<string, DiagramPointPosition> = isRectangle(figure) ? { ...fallbackPositions } : {}
  if (isRectangle(figure)) {
    const height = 132
    const width = height * rectangleAspect(figure)
    const left = 124
    const bottom = 170
    positions.A = { x: left, y: bottom }
    positions.B = { x: left + width, y: bottom }
    positions.D = { x: left, y: bottom - height }
    positions.C = { x: left + width, y: bottom - height }
  } else if (isTriangle(figure)) {
    seedTriangle(figure, positions)
  }

  const anchors = figure.layoutHints?.anchors
  if (anchors) {
    for (const [point, anchor] of Object.entries(anchors)) {
      if (anchor.x >= 0 && anchor.x <= 1 && anchor.y >= 0 && anchor.y <= 1) {
        positions[point] = {
          x: PADDING + anchor.x * (VIEW_WIDTH - PADDING * 2),
          y: PADDING + anchor.y * (VIEW_HEIGHT - PADDING * 2),
        }
      }
    }
  }
  applyExplicitCoordinates(figure, positions)
  return positions
}

function hasSegment(figure: DiagramFigure, expected: string) {
  return (figure.segments ?? []).some((segment) => normalizedSegment(segment) === normalizedSegment(expected))
}

function isTriangle(figure: DiagramFigure) {
  const points = new Set(figure.points ?? [])
  return ['A', 'B', 'C'].every((point) => points.has(point)) && ['AB', 'BC', 'AC'].every((segment) => hasSegment(figure, segment))
}

function rightAnglePoint(figure: DiagramFigure) {
  // 只把三角形顶点的直角当作三角形朝向依据；辅助垂线（如 DF⊥DE）的垂足不能决定三角形布局。
  const vertices = new Set(['A', 'B', 'C'])
  for (const constraint of figure.constraints ?? []) {
    const type = constraint.type.toLowerCase()
    if (!type.includes('right') && !type.includes('perpendicular')) continue
    const point = constraint.points?.length === 3 ? constraint.points[1] : constraint.points?.[0]
    if (point) {
      const candidate = point.toUpperCase()
      if (vertices.has(candidate)) return candidate
      continue
    }
    const lines = constraint.lines ?? []
    const first = endpoints(lines[0] ?? '')
    const second = endpoints(lines[1] ?? '')
    const common = first?.find((item) => second?.includes(item))
    if (common && vertices.has(common.toUpperCase())) return common.toUpperCase()
  }
  return null
}

function seedTriangle(figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  const rightAngle = rightAnglePoint(figure)
  // 没有直角约束时只生成稳定的普通三角形；不能凭视觉比例擅自推断直角。
  if (!rightAngle) {
    positions.A = { x: 70, y: 170 }
    positions.B = { x: 220, y: 170 }
    positions.C = { x: 145, y: 45 }
    return
  }
  const { shortLeg, longLeg } = triangleLegDimensions(figure)
  const legWidth = shortLeg
  const legHeight = longLeg
  const vertices: Record<string, DiagramPointPosition> = {
    A: { x: 75, y: 165 },
    B: { x: 75 + legWidth, y: 165 },
    C: { x: 75 + legWidth, y: 165 - legHeight },
  }
  if (rightAngle === 'A') {
    vertices.A = { x: 75, y: 165 }
    vertices.B = { x: 75 + legWidth, y: 165 }
    vertices.C = { x: 75, y: 165 - legHeight }
  } else if (rightAngle === 'C') {
    // layoutHints 只作弱提示；模型方向描述可能与原图镜像，结构约束优先。
    vertices.A = { x: 75 + legWidth, y: 165 - legHeight }
    vertices.B = { x: 75, y: 165 }
    vertices.C = { x: 75 + legWidth, y: 165 }
  }
  Object.assign(positions, vertices)
}

function triangleLegDimensions(figure: DiagramFigure) {
  for (const constraint of figure.constraints ?? []) {
    if (constraint.type !== 'length_compare' || constraint.segments?.length !== 2) continue
    if (constraint.operator === '>') return { shortLeg: 105, longLeg: 135 }
    if (constraint.operator === '<') return { shortLeg: 105, longLeg: 135 }
  }
  return { shortLeg: 115, longLeg: 125 }
}

function applyExplicitCoordinates(figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  const coordinates = Object.entries(figure.coordinates ?? {})
    .map(([point, coordinate]) => ({ point, x: Number(coordinate.x), y: Number(coordinate.y) }))
    .filter((item) => Number.isFinite(item.x) && Number.isFinite(item.y))
  if (coordinates.length < 2) return

  // 明确坐标是题目数据，优先级高于示意布局；归一化只改变画布位置，不改变点间相对比例。
  const minX = Math.min(...coordinates.map((item) => item.x))
  const maxX = Math.max(...coordinates.map((item) => item.x))
  const minY = Math.min(...coordinates.map((item) => item.y))
  const maxY = Math.max(...coordinates.map((item) => item.y))
  const spanX = Math.max(maxX - minX, 1)
  const spanY = Math.max(maxY - minY, 1)
  const scale = Math.min((VIEW_WIDTH - PADDING * 2) / spanX, (VIEW_HEIGHT - PADDING * 2) / spanY)
  const offsetX = (VIEW_WIDTH - (maxX - minX) * scale) / 2
  const offsetY = (VIEW_HEIGHT - (maxY - minY) * scale) / 2
  for (const item of coordinates) {
    positions[item.point] = {
      x: offsetX + (item.x - minX) * scale,
      y: offsetY + (maxY - item.y) * scale,
    }
  }
}

function hasRectanglePoints(figure: DiagramFigure) {
  const points = new Set(figure.points ?? [])
  return ['A', 'B', 'C', 'D'].every((point) => points.has(point)) && (figure.segments ?? []).some((segment) => normalizedSegment(segment) === 'AB')
}

function isRectangle(figure: DiagramFigure) {
  const shape = figure.shape?.toLowerCase()
  return shape === 'rectangle' || shape === 'rect' || shape?.includes('矩形') === true || hasRectanglePoints(figure)
}

function addDerivedPositions(figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  let fallbackIndex = 0
  for (const point of figure.points ?? []) {
    if (!positions[point]) {
      positions[point] = { x: 40 + (fallbackIndex++ % 6) * 42, y: 90 + Math.floor(fallbackIndex / 6) * 30 }
    }
  }
  for (const group of figure.collinear ?? []) {
    placeCollinearPoints(group, figure, positions)
  }
  for (const constraint of figure.constraints ?? []) {
    if (constraint.type.toLowerCase().includes('midpoint')) placeMidpoint(constraint, positions)
  }
  for (const constraint of figure.constraints ?? []) {
    if (!constraint.type.toLowerCase().includes('perpendicular')) continue
    placePerpendicularIntersection(constraint, figure, positions)
  }
  for (let pass = 0; pass < 2; pass += 1) {
    for (const item of figure.intersections ?? []) {
      const first = endpoints(item.of?.[0] ?? '')
      const second = endpoints(item.of?.[1] ?? '')
      if (!first || !second) continue
      if (!positions[first[0]] || !positions[first[1]] || !positions[second[0]] || !positions[second[1]]) continue
      const point = intersection([positions[first[0]], positions[first[1]]], [positions[second[0]], positions[second[1]]])
      if (point) positions[item.point] = point
    }
  }
}

type DiagramConstraintValue = NonNullable<DiagramFigure['constraints']>[number]

function placeMidpoint(constraint: DiagramConstraintValue, positions: Record<string, DiagramPointPosition>) {
  const midpoint = constraint.point ?? constraint.points?.[0]
  const segment = constraint.segment ?? constraint.segments?.[0]
  const endpointsOfSegment = segment ? endpoints(segment) : null
  if (!midpoint || !endpointsOfSegment || !positions[endpointsOfSegment[0]] || !positions[endpointsOfSegment[1]]) return
  positions[midpoint] = {
    x: (positions[endpointsOfSegment[0]].x + positions[endpointsOfSegment[1]].x) / 2,
    y: (positions[endpointsOfSegment[0]].y + positions[endpointsOfSegment[1]].y) / 2,
  }
}

function placePerpendicularIntersection(constraint: DiagramConstraintValue, figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  const points = constraint.points ?? []
  if (points.length < 3 || !positions[points[0]] || !positions[points[1]]) return
  const target = points[2]
  const collinear = (figure.collinear ?? []).find((group) => group.includes(target) && group.filter((point) => positions[point]).length >= 3)
  if (!collinear) return
  const base = collinear.filter((point) => point !== target && positions[point])
  if (base.length < 2) return
  const first = positions[points[0]]
  const second = positions[points[1]]
  const perpendicularEnd = { x: first.x - (second.y - first.y), y: first.y + (second.x - first.x) }
  const hit = intersection([first, perpendicularEnd], [positions[base[0]], positions[base[1]]])
  // BC 是边而不是无限直线；交点超出端点时不能把 F 绘制到延长线上。
  if (hit && isOnSegment(hit, positions[base[0]], positions[base[1]])) positions[target] = hit
}

function isOnSegment(point: DiagramPointPosition, first: DiagramPointPosition, second: DiagramPointPosition) {
  const cross = (point.x - first.x) * (second.y - first.y) - (point.y - first.y) * (second.x - first.x)
  if (Math.abs(cross) > 0.01) return false
  const within = (value: number, start: number, end: number) => value >= Math.min(start, end) - 0.01 && value <= Math.max(start, end) + 0.01
  return within(point.x, first.x, second.x) && within(point.y, first.y, second.y)
}

function placeCollinearPoints(group: string[], figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  if (group.length < 3) return
  const anchors = group.filter((point) => positions[point])
  if (anchors.length < 2) return
  const first = positions[anchors[0]]
  const second = positions[anchors[anchors.length - 1]]
  const extension = isExtensionOfLine(group, figure)
  for (const [index, point] of group.entries()) {
    if (positions[point]) continue
    const ratio = extension ? -0.35 : index / (group.length - 1)
    positions[point] = {
      x: first.x + (second.x - first.x) * ratio,
      y: first.y + (second.y - first.y) * ratio,
    }
  }
}

// isExtensionOfLine 只有当“延长线”约束明确给出的直线两端点都落在本共线组内时，才判定该点位于反向延长方向。
// 否则会像“E 在 CA 延长线上”被错误套用到包含 A、B 的共线组上，把 E 画到 AB 的反向延长线上。
function isExtensionOfLine(group: string[], figure: DiagramFigure) {
  const groupSet = new Set(group.map((point) => point.toUpperCase()))
  return (figure.constraints ?? []).some((constraint) => {
    if (!constraint.type.toLowerCase().includes('extension')) return false
    const line = endpoints(constraint.lines?.[0] ?? constraint.segments?.[0] ?? '')
    if (!line) return false
    return line.every((point) => groupSet.has(point.toUpperCase()))
  })
}

export function segmentPoints(segment: string, positions: Record<string, DiagramPointPosition>) {
  const points = endpoints(segment)
  if (!points || !positions[points[0]] || !positions[points[1]]) return null
  return [positions[points[0]], positions[points[1]]] as const
}

function isSubsegment(candidate: string, parent: string, positions: Record<string, DiagramPointPosition>) {
  const child = segmentPoints(candidate, positions)
  const whole = segmentPoints(parent, positions)
  if (!child || !whole || candidate === parent) return false
  const [a, b] = child
  const [c, d] = whole
  const cross = (b.x - a.x) * (d.y - c.y) - (b.y - a.y) * (d.x - c.x)
  if (Math.abs(cross) > 0.001) return false
  const within = (value: number, start: number, end: number) => value >= Math.min(start, end) - 0.001 && value <= Math.max(start, end) + 0.001
  return within(a.x, c.x, d.x) && within(a.y, c.y, d.y) && within(b.x, c.x, d.x) && within(b.y, c.y, d.y)
}

function visibleSegments(figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  const segments = new Set<string>()
  for (const raw of figure.segments ?? []) {
    const points = endpoints(raw)
    if (points) segments.add(points.slice().sort().join(''))
  }
  for (const item of figure.intersections ?? []) {
    for (const raw of item.of ?? []) {
      const points = endpoints(raw)
      if (points) segments.add(points.slice().sort().join(''))
    }
  }
  const all = [...segments]
  return all.filter((segment) => !all.some((parent) => isSubsegment(segment, parent, positions)))
}

function visibleCurves(figure: DiagramFigure, positions: Record<string, DiagramPointPosition>) {
  return (figure.curves ?? []).flatMap((curve, index) => {
    if (curve.type !== 'arc' || curve.endpoints?.length !== 2) return []
    const [from, to] = curve.endpoints
    if (!positions[from] || !positions[to]) return []
    const radius = Math.hypot(positions[to].x - positions[from].x, positions[to].y - positions[from].y) / 2
    if (!Number.isFinite(radius) || radius <= 0) return []
    return [{
      id: `curve-${index}-${from}-${to}`,
      from: positions[from],
      to: positions[to],
      radius,
      sweep: (curve.sweep === 'lower' ? 'lower' : 'upper') as 'upper' | 'lower',
    }]
  })
}

export function layoutFigures(figures: DiagramFigure[]): RenderedDiagramFigure[] {
  return figures.map((figure) => {
    const positions = seedPositions(figure)
    addDerivedPositions(figure, positions)
    const segments = visibleSegments(figure, positions)
    const lines = segments.flatMap((segment) => {
      const points = segmentPoints(segment, positions)
      return points ? [{ id: segment, from: points[0], to: points[1] }] : []
    })
    const curves = visibleCurves(figure, positions)
    return { figure, positions, segments, lines, curves }
  })
}
