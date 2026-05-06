import React from 'react';
import { User } from '../types/user';
import { formatDate } from '../utils/date';

interface UserCardProps {
  user: User;
  onSelect?: (userId: number) => void;
}

export const UserCard: React.FC<UserCardProps> = ({ user, onSelect }) => {
  const handleClick = () => {
    onSelect?.(user.id);
  };

  return (
    <div className="user-card" onClick={handleClick}>
      <h3>{user.name}</h3>
      <p>{user.email}</p>
      <span>{formatDate(user.createdAt)}</span>
    </div>
  );
};
