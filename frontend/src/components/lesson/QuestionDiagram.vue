<script setup lang="ts">
import { computed } from 'vue'
import type { DiagramFigure, QuestionDiagram } from '../../types/lesson'
import { layoutFigures } from './diagramLayout'

const props = defineProps<{
  diagram: QuestionDiagram | null
}>()

const baseFigures = computed<DiagramFigure[]>(() => {
  return props.diagram?.figures ?? []
})

const renderedFigures = computed(() => layoutFigures(baseFigures.value))
</script>

<template>
  <div v-if="renderedFigures.length" class="question-diagram-list">
    <div v-for="rendered in renderedFigures" :key="rendered.figure.id" class="question-diagram">
      <div class="question-diagram__title">{{ rendered.figure.title || rendered.figure.id }}</div>
      <svg viewBox="0 0 280 205" role="img" :aria-label="rendered.figure.title || rendered.figure.id">
        <line
          v-for="line in rendered.lines"
          :key="line.id"
          :x1="line.from.x"
          :y1="line.from.y"
          :x2="line.to.x"
          :y2="line.to.y"
          stroke="#263238"
          stroke-width="1.6"
        />
        <!-- SVG 的 sweep-flag=1 在当前屏幕坐标系中绘制上方弧线；不能直接把领域层 upper 映射为 0。 -->
        <path
          v-for="curve in rendered.curves"
          :key="curve.id"
          :d="`M ${curve.from.x} ${curve.from.y} A ${curve.radius} ${curve.radius} 0 0 ${curve.sweep === 'upper' ? 1 : 0} ${curve.to.x} ${curve.to.y}`"
          fill="none"
          stroke="#263238"
          stroke-width="1.6"
        />
        <g v-for="point in rendered.figure.points || []" :key="point">
          <circle
            v-if="rendered.positions[point]"
            :cx="rendered.positions[point].x"
            :cy="rendered.positions[point].y"
            r="3.2"
            fill="#e97861"
          />
          <text
            v-if="rendered.positions[point]"
            :x="rendered.positions[point].x + 5"
            :y="rendered.positions[point].y - 5"
            fill="#263238"
            font-size="11"
          >{{ point }}</text>
        </g>
      </svg>
    </div>
  </div>
</template>

<style scoped>
.question-diagram-list {
  display: grid;
  gap: 16px;
}

.question-diagram {
  overflow: hidden;
  border: 1px solid #eadfce;
  border-radius: 14px;
  background: #fffaf3;
}

.question-diagram__title {
  padding: 12px 16px 0;
  color: #8d8174;
  font-size: 14px;
}

svg {
  display: block;
  width: 100%;
  height: auto;
  min-height: 190px;
}
</style>
