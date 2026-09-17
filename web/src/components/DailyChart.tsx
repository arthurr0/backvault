import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { ChartTooltip } from '@/components/Chart'
import { formatBytes, formatDay } from '@/lib/format'
import type { DailyStat } from '@/api/types'

export interface ChartPoint extends DailyStat {
  gib: number
}

interface ScaledPoint extends ChartPoint {
  scaled: number
}

const BYTE_SCALES: Array<{ unit: string; divisor: number }> = [
  { unit: 'GiB', divisor: 1024 ** 3 },
  { unit: 'MiB', divisor: 1024 ** 2 },
  { unit: 'KiB', divisor: 1024 },
  { unit: 'B', divisor: 1 },
]

function pickScale(points: ChartPoint[]): { unit: string; divisor: number } {
  const peak = points.reduce((max, point) => Math.max(max, point.bytes), 0)
  for (const scale of BYTE_SCALES) {
    if (peak >= scale.divisor) return scale
  }
  return BYTE_SCALES[BYTE_SCALES.length - 1]
}

export function DailyChart({ points }: { points: ChartPoint[] }) {
  const scale = pickScale(points)
  const scaled: ScaledPoint[] = points.map((point) => ({
    ...point,
    scaled: Number((point.bytes / scale.divisor).toFixed(2)),
  }))
  return (
    <div className="h-72 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <ComposedChart data={scaled} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
                <CartesianGrid stroke="var(--border)" vertical={false} />
                <XAxis
                  dataKey="date"
                  tickFormatter={formatDay}
                  tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
                  tickLine={false}
                  axisLine={{ stroke: 'var(--border)' }}
                  minTickGap={18}
                />
                <YAxis
                  yAxisId="runs"
                  allowDecimals={false}
                  tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  yAxisId="bytes"
                  orientation="right"
                  tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(value: number) => `${value} ${scale.unit}`}
                  width={64}
                />
                <Tooltip
                  cursor={{ fill: 'var(--surface-3)', opacity: 0.5 }}
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null
                    const point = payload[0].payload as ScaledPoint
                    return (
                      <ChartTooltip
                        title={formatDay(String(label))}
                        rows={[
                          { label: 'Success', value: String(point.success), color: 'var(--success)' },
                          { label: 'Failed', value: String(point.failed), color: 'var(--danger)' },
                          { label: 'Uploaded', value: formatBytes(point.bytes), color: 'var(--brass)' },
                        ]}
                      />
                    )
                  }}
                />
                <Legend
                  wrapperStyle={{ fontSize: 11, color: 'var(--text-muted)' }}
                  iconType="circle"
                  iconSize={7}
                />
                <Bar
                  yAxisId="runs"
                  dataKey="success"
                  name="Success"
                  stackId="runs"
                  fill="var(--success)"
                  radius={[0, 0, 0, 0]}
                  maxBarSize={18}
                />
                <Bar
                  yAxisId="runs"
                  dataKey="failed"
                  name="Failed"
                  stackId="runs"
                  fill="var(--danger)"
                  radius={[3, 3, 0, 0]}
                  maxBarSize={18}
                />
                <Line
                  yAxisId="bytes"
                  type="monotone"
                  dataKey="scaled"
                  name={`Uploaded (${scale.unit})`}
                  stroke="var(--brass)"
                  strokeWidth={2}
                  dot={false}
                />
              </ComposedChart>
            </ResponsiveContainer>
          </div>
  )
}
