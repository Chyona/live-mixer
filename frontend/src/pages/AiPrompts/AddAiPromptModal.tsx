import AiPromptFormModal from '~/components/AiPromptFormModal';

interface AddAiPromptModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const AddAiPromptModal = ({ open, onClose, onSuccess }: AddAiPromptModalProps) => {
  return (
    <AiPromptFormModal
      open={open}
      onClose={onClose}
      onSuccess={() => onSuccess()}
    />
  );
};

export default AddAiPromptModal;
