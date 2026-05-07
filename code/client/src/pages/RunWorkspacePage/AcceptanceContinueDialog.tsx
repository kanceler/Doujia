import { useEffect, useMemo, useState } from 'react';
import type { DevflowImprovementItemView } from '@shared/devflow-api';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Textarea } from '@/components/ui/textarea';
import { ScrollArea } from '@/components/ui/scroll-area';

interface AcceptanceContinueDialogProps {
  open: boolean;
  items: DevflowImprovementItemView[];
  currentIterationNo: number;
  loading?: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (payload: { selected_item_ids: string[]; freeform_text: string }) => Promise<void> | void;
}

export const AcceptanceContinueDialog: React.FC<AcceptanceContinueDialogProps> = ({
  open,
  items,
  currentIterationNo,
  loading = false,
  onOpenChange,
  onSubmit,
}) => {
  const selectableItems = useMemo(
    () => items.filter((item) => item.status === 'open' || (item.status === 'selected' && item.iteration_no === currentIterationNo)),
    [currentIterationNo, items],
  );
  const [selectedItemIds, setSelectedItemIds] = useState<string[]>(() =>
    selectableItems.filter((item) => item.status === 'selected').map((item) => item.item_id),
  );
  const [freeformText, setFreeformText] = useState('');

  useEffect(() => {
    if (!open) return;
    setSelectedItemIds(
      selectableItems.filter((item) => item.status === 'selected').map((item) => item.item_id),
    );
  }, [open, selectableItems]);

  const toggleSelection = (itemId: string, checked: boolean) => {
    setSelectedItemIds((current) => (
      checked
        ? [...new Set([...current, itemId])]
        : current.filter((value) => value !== itemId)
    ));
  };

  const handleSubmit = async () => {
    await onSubmit({
      selected_item_ids: selectedItemIds,
      freeform_text: freeformText.trim(),
    });
    setFreeformText('');
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Continue the next iteration</DialogTitle>
          <DialogDescription>
            Select the improvement items that should flow into the next iteration, then add any fresh notes from the current acceptance checkpoint.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <ScrollArea className="max-h-[320px] rounded-md border border-border">
            <div className="space-y-2 p-3">
              {selectableItems.length === 0 && (
                <div className="rounded-md bg-muted/40 px-3 py-4 text-sm text-muted-foreground">
                  No open improvement items yet. You can still continue with freeform text below.
                </div>
              )}
              {selectableItems.map((item) => {
                const checked = selectedItemIds.includes(item.item_id);
                return (
                  <label
                    key={item.item_id}
                    className="flex cursor-pointer items-start gap-3 rounded-md border border-border bg-background px-3 py-2"
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(value) => toggleSelection(item.item_id, value === true)}
                    />
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-foreground">{item.title}</div>
                      <div className="mt-1 text-xs text-muted-foreground">{item.detail || 'No detail'}</div>
                    </div>
                  </label>
                );
              })}
            </div>
          </ScrollArea>

          <div className="space-y-2">
            <div className="text-sm font-medium text-foreground">Additional requirement notes</div>
            <Textarea
              rows={5}
              placeholder="Describe any new adjustments that should be folded into the next CEO requirement document."
              value={freeformText}
              onChange={(event) => setFreeformText(event.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={() => void handleSubmit()} disabled={loading}>
            {loading ? 'Continuing' : 'Continue iteration'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
