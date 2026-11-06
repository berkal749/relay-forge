import { useState } from "react"
import { Input } from "@/components/ui/input"
import {
  CUSTOM_JOB_TYPE,
  JOB_TYPES,
  JOB_TYPE_GROUPS,
  findJobType,
} from "@/lib/job-types"

export function JobTypeSelect({
  id,
  value,
  onChange,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
}) {
  const known = findJobType(value)
  const [customMode, setCustomMode] = useState(!known)
  const [customDraft, setCustomDraft] = useState(known ? "" : value)
  const custom = customMode || !known

  function handleSelect(next: string) {
    if (next === CUSTOM_JOB_TYPE) {
      setCustomMode(true)
      setCustomDraft("")
      onChange("")
      return
    }
    setCustomMode(false)
    onChange(next)
  }

  const definition = known && !custom ? known : undefined

  return (
    <div className="space-y-2">
      <select
        id={id}
        className="h-9 w-full rounded-md border bg-background px-3 text-sm"
        value={custom ? CUSTOM_JOB_TYPE : value}
        onChange={(event) => handleSelect(event.target.value)}
      >
        {JOB_TYPE_GROUPS.map((group) => (
          <optgroup key={group} label={group}>
            {JOB_TYPES.filter((item) => item.group === group).map((item) => (
              <option key={item.type} value={item.type}>
                {item.label} — {item.type}
              </option>
            ))}
          </optgroup>
        ))}
        <optgroup label="Custom">
          <option value={CUSTOM_JOB_TYPE}>Custom job type…</option>
        </optgroup>
      </select>

      {custom ? (
        <Input
          aria-label="Custom job type"
          placeholder="my_handler"
          value={customDraft}
          onChange={(event) => {
            setCustomDraft(event.target.value)
            onChange(event.target.value)
          }}
          maxLength={100}
          required
        />
      ) : definition ? (
        <p className="text-xs text-muted-foreground">{definition.description}</p>
      ) : null}
    </div>
  )
}
