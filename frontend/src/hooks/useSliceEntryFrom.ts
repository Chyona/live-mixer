import { useMemo } from 'react';
import { useLocation } from 'react-router-dom';
import { getSliceEditorEntryFrom, type SliceEditorEntryFrom } from '~/routes/links';

export function useSliceEntryFrom(defaultFrom: SliceEditorEntryFrom = 'source-videos') {
  const location = useLocation();

  return useMemo(() => {
    return getSliceEditorEntryFrom(location.state) ?? defaultFrom;
  }, [defaultFrom, location.state]);
}
