// Public surface of @skinloom/ui. Plugins import from here, so every export is API:
// add freely, but renames and removals follow the plugin API deprecation policy.
//
// Base components come from shadcn/ui (Base UI primitives), adjusted to web/design.md;
// SkinLoom's own components live in ./components/skinloom (ADR 0042).
export { cn } from './lib/utils'
export { MenuIcon, menuIcons } from './icons'
export { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from './components/ui/accordion'
export { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogMedia, AlertDialogOverlay, AlertDialogPortal, AlertDialogTitle, AlertDialogTrigger } from './components/ui/alert-dialog'
export { Alert, AlertTitle, AlertDescription, AlertAction } from './components/ui/alert'
export { Avatar, AvatarImage, AvatarFallback, AvatarGroup, AvatarGroupCount, AvatarBadge } from './components/ui/avatar'
export { Badge, badgeVariants } from './components/ui/badge'
export { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink, BreadcrumbPage, BreadcrumbSeparator, BreadcrumbEllipsis } from './components/ui/breadcrumb'
export { ButtonGroup, ButtonGroupSeparator, ButtonGroupText, buttonGroupVariants } from './components/ui/button-group'
export { Button, ButtonLink, buttonVariants, type ButtonProps } from './components/ui/button'
export { Card, CardHeader, CardFooter, CardTitle, CardAction, CardDescription, CardContent } from './components/ui/card'
export { Checkbox } from './components/ui/checkbox'
export { Collapsible, CollapsibleTrigger, CollapsibleContent } from './components/ui/collapsible'
export { Combobox, ComboboxInput, ComboboxContent, ComboboxList, ComboboxItem, ComboboxGroup, ComboboxLabel, ComboboxCollection, ComboboxEmpty, ComboboxSeparator, ComboboxChips, ComboboxChip, ComboboxChipsInput, ComboboxTrigger, ComboboxValue, useComboboxAnchor } from './components/ui/combobox'
export { Command, CommandDialog, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem, CommandShortcut, CommandSeparator } from './components/ui/command'
export { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogOverlay, DialogPortal, DialogTitle, DialogTrigger } from './components/ui/dialog'
export { Drawer, DrawerPortal, DrawerOverlay, DrawerSwipeHandle, DrawerTrigger, DrawerClose, DrawerContent, DrawerHeader, DrawerFooter, DrawerTitle, DrawerDescription } from './components/ui/drawer'
export { DropdownMenu, DropdownMenuPortal, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuGroup, DropdownMenuLabel, DropdownMenuItem, DropdownMenuCheckboxItem, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent } from './components/ui/dropdown-menu'
export { Empty, EmptyHeader, EmptyTitle, EmptyDescription, EmptyContent, EmptyMedia } from './components/ui/empty'
export { Field, FieldLabel, FieldDescription, FieldError, FieldGroup, FieldLegend, FieldSeparator, FieldSet, FieldContent, FieldTitle } from './components/ui/field'
export { InputGroup, InputGroupAddon, InputGroupButton, InputGroupText, InputGroupInput, InputGroupTextarea } from './components/ui/input-group'
export { Input } from './components/ui/input'
export { Item, ItemMedia, ItemContent, ItemActions, ItemGroup, ItemSeparator, ItemTitle, ItemDescription, ItemHeader, ItemFooter } from './components/ui/item'
export { Kbd, KbdGroup } from './components/ui/kbd'
export { Label } from './components/ui/label'
export { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from './components/ui/pagination'
export { Popover, PopoverContent, PopoverDescription, PopoverHeader, PopoverTitle, PopoverTrigger } from './components/ui/popover'
export { Progress, ProgressTrack, ProgressIndicator, ProgressLabel, ProgressValue } from './components/ui/progress'
export { RadioGroup, RadioGroupItem } from './components/ui/radio-group'
export { ScrollArea, ScrollBar } from './components/ui/scroll-area'
export { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectScrollDownButton, SelectScrollUpButton, SelectSeparator, SelectTrigger, SelectValue } from './components/ui/select'
export { Separator } from './components/ui/separator'
export { Sheet, SheetTrigger, SheetClose, SheetContent, SheetHeader, SheetFooter, SheetTitle, SheetDescription } from './components/ui/sheet'
export { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupAction, SidebarGroupContent, SidebarGroupLabel, SidebarHeader, SidebarInput, SidebarInset, SidebarMenu, SidebarMenuAction, SidebarMenuBadge, SidebarMenuButton, SidebarMenuItem, SidebarMenuSkeleton, SidebarMenuSub, SidebarMenuSubButton, SidebarMenuSubItem, SidebarProvider, SidebarRail, SidebarSeparator, SidebarTrigger, useSidebar } from './components/ui/sidebar'
export { Skeleton } from './components/ui/skeleton'
export { Slider } from './components/ui/slider'
export { Toaster } from './components/ui/sonner'
export { Spinner } from './components/ui/spinner'
export { Switch } from './components/ui/switch'
export { Table, TableHeader, TableBody, TableFooter, TableHead, TableRow, TableCell, TableCaption } from './components/ui/table'
export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants } from './components/ui/tabs'
export { Textarea } from './components/ui/textarea'
export { ToggleGroup, ToggleGroupItem } from './components/ui/toggle-group'
export { Toggle, toggleVariants } from './components/ui/toggle'
export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from './components/ui/tooltip'
// Charts (Recharts) are not in the barrel: import '@skinloom/ui/components/ui/chart' where needed.

// SkinLoom's own components (web/design.md §10.3).
export { avatarUrl, previewUrl, previewWidth } from './lib/render'
export { type ConfirmOptions, ConfirmProvider, useConfirm } from './components/skinloom/confirm'
export { CodeBlock, CopyField } from './components/skinloom/copy-field'
export { Dropzone } from './components/skinloom/dropzone'
export { defaultLabels, fill, type UiLabels, UiLabelsProvider, useUiLabels } from './components/skinloom/labels'
export { Markdown, PageHeader, StatCard, type Status, StatusDot, UsageMeter } from './components/skinloom/layout'
export { PixelBackdrop } from './components/skinloom/pixel-backdrop'
export { formatRelative, RelativeTime } from './components/skinloom/relative-time'
export {
  ResponsiveDialog,
  ResponsiveDialogClose,
  ResponsiveDialogContent,
  ResponsiveDialogDescription,
  ResponsiveDialogFooter,
  ResponsiveDialogHeader,
  ResponsiveDialogTitle,
  ResponsiveDialogTrigger,
} from './components/skinloom/responsive-dialog'
export { SaveBar } from './components/skinloom/save-bar'
export { useIsMobile } from './hooks/use-mobile'
export { type CarouselSkin, SkinCarousel } from './components/skinloom/skin-carousel'
export { prefersReducedMotion, SkinViewer, type SkinViewerAnimation, type SkinViewerBackground } from './components/skinloom/skin-viewer'
export { PlayerCard, TextureCard, type TextureCardTexture } from './components/skinloom/texture-card'
export { type PickableTexture, TexturePicker } from './components/skinloom/texture-picker'
export { type TextureType, TexturePreview } from './components/skinloom/texture-preview'
export { UserAvatar } from './components/skinloom/user-avatar'
export { toast } from 'sonner'
// Gives Base UI's inline <script>/<style> the page's CSP nonce (ADR 0039); the host wraps the app in it.
export { CSPProvider } from '@base-ui/react/csp-provider'
